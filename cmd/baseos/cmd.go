package baseos

import (
	"context"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/combust-labs/firebuild/cmd"
	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/build/reader"
	"github.com/combust-labs/firebuild/pkg/build/stage"
	"github.com/combust-labs/firebuild/pkg/containers"
	"github.com/combust-labs/firebuild/pkg/metadata"
	"github.com/combust-labs/firebuild/pkg/naming"
	"github.com/combust-labs/firebuild/pkg/storage"
	"github.com/combust-labs/firebuild/pkg/tracing"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/opentracing/opentracing-go"
	"github.com/spf13/cobra"
)

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "baseos",
	Short: "Build a base operating system image",
	Run:   run,
	Long:  ``,
}

var (
	commandConfig = configs.NewBaseOSCommandConfig()
	logConfig     = configs.NewLogginConfig()
	tracingConfig = configs.NewTracingConfig("firebuild-baseos")
)

func initFlags() {
	Command.Flags().AddFlagSet(commandConfig.FlagSet())
	Command.Flags().AddFlagSet(logConfig.FlagSet())
	Command.Flags().AddFlagSet(tracingConfig.FlagSet())
	// Storage provider flags:
	cmd.AddStorageFlags(Command.Flags())
}

func init() {
	initFlags()
}

func run(cobraCommand *cobra.Command, _ []string) {
	os.Exit(processCommand())
}

func processCommand() int {

	cleanup := utils.NewDefers()
	defer cleanup.CallAll()

	rootLogger := logConfig.NewLogger("baseos")

	// tracing:

	rootLogger.Info("configuring tracing", "enabled", tracingConfig.Enable, "application-name", tracingConfig.ApplicationName)

	tracer, tracerCleanupFunc, tracerErr := tracing.GetTracer(rootLogger.Named("tracer"), tracingConfig)
	if tracerErr != nil {
		rootLogger.Error("failed constructing tracer", "reason", tracerErr)
		return 1
	}

	cleanup.Add(tracerCleanupFunc)

	spanBuild := tracer.StartSpan("build-baseos")
	cleanup.Add(func() {
		spanBuild.Finish()
	})

	dockerStat, statErr := os.Stat(commandConfig.Dockerfile)
	if statErr != nil {
		rootLogger.Error("error while resolving --dockerfile path", "reason", statErr)
		spanBuild.SetBaggageItem("error", statErr.Error())
		return 1
	}
	if dockerStat.IsDir() {
		err := fmt.Errorf("--dockerfile points at a directory")
		rootLogger.Error(err.Error())
		spanBuild.SetBaggageItem("error", err.Error())
		return 1
	}

	if commandConfig.Tag != "" {
		if !utils.IsValidTag(commandConfig.Tag) {
			rootLogger.Error("--tag value is invalid", "tag", commandConfig.Tag)
			spanBuild.SetBaggageItem("error", fmt.Errorf("--tag value is invalid: '%s'", commandConfig.Tag).Error())
			return 1
		}
	}

	storageImpl, resolveErr := cmd.GetStorageImpl(rootLogger)
	if resolveErr != nil {
		rootLogger.Error("failed resolving storage provider", "reason", resolveErr)
		spanBuild.SetBaggageItem("error", resolveErr.Error())
		return 1
	}

	spanTempDir := tracer.StartSpan("baseos-temp-dir", opentracing.ChildOf(spanBuild.Context()))

	tempDirectory, err := ioutil.TempDir("", "")
	if err != nil {
		rootLogger.Error("failed creating temporary build directory", "reason", err)
		spanTempDir.SetBaggageItem("error", err.Error())
		spanTempDir.Finish()
		return 1
	}
	cleanup.Add(func() {
		span := tracer.StartSpan("baseos-temp-dir", opentracing.ChildOf(spanTempDir.Context()))
		if err := os.RemoveAll(tempDirectory); err != nil {
			rootLogger.Error("failed cleaning up temporary build directory", "reason", err)
			span.SetBaggageItem("error", err.Error())
		}
		span.Finish()
	})

	spanTempDir.Finish()

	spanParseDockerfile := tracer.StartSpan("baseos-parse-dockerfile", opentracing.ChildOf(spanTempDir.Context()))

	// we parse the file to establish the base operating system we build
	// we must enforce some constants so we assume here
	// no multi-stage builds - only main stage
	readResults, err := reader.ReadFromString(commandConfig.Dockerfile, tempDirectory)
	if err != nil {
		rootLogger.Error("failed parsing Dockerfile", "reason", err)
		spanParseDockerfile.SetBaggageItem("error", err.Error())
		spanParseDockerfile.Finish()
		return 1
	}

	spanParseDockerfile.Finish()

	spanReadStages := tracer.StartSpan("baseos-read-stages", opentracing.ChildOf(spanParseDockerfile.Context()))

	scs, errs := stage.ReadStages(readResults.Commands())
	for _, err := range errs {
		rootLogger.Warn("stages read contained an error", "reason", err)
	}

	if len(scs.Unnamed()) != 1 {
		rootLogger.Error("Dockerfile must contain exactly one unnamed FROM build stage")
		spanReadStages.SetBaggageItem("error", "invalid unnamed count")
		spanReadStages.Finish()
		return 1
	}

	if len(scs.Named()) > 0 {
		rootLogger.Error("Dockerfile contains other named stages, this is not supported")
		spanReadStages.SetBaggageItem("error", "has named stages")
		spanReadStages.Finish()
		return 1
	}

	// find out what OS are we building:
	fromFound := false
	fromToBuild := commands.From{}
	for _, cmd := range scs.Unnamed()[0].Commands() {
		switch tcmd := cmd.(type) {
		case commands.From:
			fromFound = true
			fromToBuild = tcmd
			break
		}
	}

	if !fromFound {
		rootLogger.Error("unnamed stage without a FROM command")
		spanReadStages.SetBaggageItem("error", "invalid unnamed without FROM")
		spanReadStages.Finish()
		return 1
	}

	spanBuild.SetTag("from", fromToBuild.BaseImage)
	spanReadStages.Finish()

	rootLogger.Info("building base operating system root file system", "os", fromToBuild.BaseImage)

	spanGetDockerClient := tracer.StartSpan("baseos-get-docker-client", opentracing.ChildOf(spanReadStages.Context()))

	// we have to build the Docker image, we can use the dependency builder here:
	client, clientErr := containers.GetDefaultClient()
	if clientErr != nil {
		rootLogger.Error("failed creating a Docker client", "reason", clientErr)
		spanGetDockerClient.SetBaggageItem("error", clientErr.Error())
		spanGetDockerClient.Finish()
		return 1
	}

	spanGetDockerClient.Finish()

	tagName := strings.ToLower(utils.RandStringBytes(32)) + ":build"

	spanBuild.SetTag("docker-tag", tagName)

	rootLogger.Info("building base operating system Docker image", "os", fromToBuild.BaseImage)

	spanDockerBuild := tracer.StartSpan("baseos-docker-build", opentracing.ChildOf(spanGetDockerClient.Context()))
	spanDockerBuild.SetTag("docker-tag", tagName)

	if err := containers.ImageBuild(context.Background(), client, rootLogger,
		filepath.Dir(commandConfig.Dockerfile), "Dockerfile", tagName); err != nil {
		rootLogger.Error("failed building base OS Docker image", "reason", err)
		spanDockerBuild.SetBaggageItem("error", err.Error())
		spanDockerBuild.Finish()
		return 1
	}

	spanDockerBuild.Finish()

	cleanup.Add(func() {
		span := tracer.StartSpan("baseos-docker-image-cleanup", opentracing.ChildOf(spanDockerBuild.Context()))
		span.SetTag("docker-tag", tagName)
		if err := containers.ImageRemove(context.Background(), client, rootLogger, tagName); err != nil {
			rootLogger.Error("failed post-build image clean up", "reason", err)
			span.SetBaggageItem("error", err.Error())
		}
		span.Finish()
	})

	spanDockerImageLookup := tracer.StartSpan("baseos-docker-lookup", opentracing.ChildOf(spanGetDockerClient.Context()))
	spanDockerImageLookup.SetTag("docker-tag", tagName)

	if _, findErr := containers.FindImageIDByTag(context.Background(), client, tagName); findErr != nil {
		// be extra careful:
		rootLogger.Error("expected docker image not found", "reason", findErr)
		spanDockerImageLookup.SetBaggageItem("error", findErr.Error())
		spanDockerImageLookup.Finish()
		return 1
	}

	spanDockerImageLookup.Finish()

	rootLogger.Info("image ready, creating EXT4 root file system file", "os", fromToBuild.BaseImage)
	rootFSFile := filepath.Join(tempDirectory, naming.RootfsFileName)

	spanCreateRootfs := tracer.StartSpan("baseos-create-rootfs", opentracing.ChildOf(spanDockerImageLookup.Context()))

	if err := utils.CreateRootFSFile(rootFSFile, commandConfig.FSSizeMBs); err != nil {
		rootLogger.Error("failed creating rootfs file", "reason", err)
		spanCreateRootfs.SetBaggageItem("error", err.Error())
		spanCreateRootfs.Finish()
		return 1
	}

	spanCreateRootfs.Finish()

	rootLogger.Info("EXT4 file created, making file system", "path", rootFSFile, "size-mb", commandConfig.FSSizeMBs)

	spanRootfsMkfs := tracer.StartSpan("baseos-rootfs-mkfs", opentracing.ChildOf(spanCreateRootfs.Context()))

	if err := utils.MkfsExt4(rootFSFile); err != nil {
		rootLogger.Error("failed creating EXT4 in rootfs file", "reason", err)
		spanRootfsMkfs.SetBaggageItem("error", err.Error())
		spanRootfsMkfs.Finish()
		return 1
	}

	spanRootfsMkfs.Finish()

	rootLogger.Info("EXT4 file system created, mouting", "path", rootFSFile, "size-mb", commandConfig.FSSizeMBs)

	spanMountRootfs := tracer.StartSpan("baseos-mount-rootfs", opentracing.ChildOf(spanRootfsMkfs.Context()))

	// create the mount directory:
	mountDir := filepath.Join(tempDirectory, "mount")
	mkdirErr := os.Mkdir(mountDir, fs.ModePerm)
	if mkdirErr != nil {
		rootLogger.Error("failed creating EXT4 mount directory", "reason", mkdirErr)
		spanMountRootfs.SetBaggageItem("error", mkdirErr.Error())
		spanMountRootfs.Finish()
		return 1
	}

	if err := utils.Mount(rootFSFile, mountDir); err != nil {
		rootLogger.Error("failed mounting rootfs file in mount dir", "reason", err)
		spanMountRootfs.SetBaggageItem("error", err.Error())
		spanMountRootfs.Finish()
		return 1
	}

	spanMountRootfs.Finish()

	rootLogger.Info("EXT4 file mounted in mount dir", "rootfs", rootFSFile, "mount-dir", mountDir)

	cleanup.Add(func() {
		span := tracer.StartSpan("baseos-unmount-rootfs", opentracing.ChildOf(spanMountRootfs.Context()))
		if err := utils.Umount(mountDir); err != nil {
			rootLogger.Error("failed unmounting rootfs mount dir", "reason", err)
			span.SetBaggageItem("error", err.Error())
		}
		span.Finish()
		rootLogger.Info("EXT4 file unmounted from mount dir", "rootfs", rootFSFile, "mount-dir", mountDir)
	})

	spanDockerImageExport := tracer.StartSpan("baseos-docker-export", opentracing.ChildOf(spanMountRootfs.Context()))

	if err := containers.ImageBaseOSExport(context.Background(), client, rootLogger, mountDir, tagName,
		tracer, spanDockerImageExport.Context()); err != nil {
		rootLogger.Error("failed building root file system for the base OS", "reason", err)
		spanDockerImageExport.SetBaggageItem("error", err.Error())
		return 1
	}

	spanDockerImageExport.Finish()

	spanRootfsPersist := tracer.StartSpan("baseos-rootfs-persist", opentracing.ChildOf(spanMountRootfs.Context()))

	structuredBase := fromToBuild.ToStructuredFrom()
	resultOrg := structuredBase.Org()
	resultImage := structuredBase.Image()
	resultVersion := structuredBase.Version()

	if commandConfig.Tag != "" {
		ok, org, image, version := utils.TagDecompose(commandConfig.Tag)
		if !ok {
			rootLogger.Error("tag defined but failed to parse", "tag", commandConfig.Tag)
			spanRootfsPersist.SetBaggageItem("error", "tag defined but failed to parse")
			spanRootfsPersist.Finish()
			return 1
		}
		resultOrg = org
		resultImage = image
		resultVersion = version
	}

	spanRootfsPersist.SetTag("tag", fmt.Sprintf("%s/%s:%s", resultOrg, resultImage, resultVersion))

	storeResult, storeErr := storageImpl.StoreRootfsFile(&storage.RootfsStore{
		LocalPath: rootFSFile,
		Metadata: metadata.MDBaseOS{
			CreatedAtUTC: time.Now().UTC().Unix(),
			Image: metadata.MDImage{
				Org:     structuredBase.Org(),
				Image:   structuredBase.Image(),
				Version: structuredBase.Version(),
			},
			Labels: map[string]string{},
			Type:   metadata.MetadataTypeBaseOS,
		},
		Org:     resultOrg,
		Image:   resultImage,
		Version: resultVersion,
	})
	if storeErr != nil {
		rootLogger.Error("failed storing built rootfs", "reason", storeErr)
		spanRootfsPersist.SetBaggageItem("error", storeErr.Error())
		spanRootfsPersist.Finish()
		return 1
	}

	spanRootfsPersist.Finish()

	rootLogger.Info("Build completed successfully. Rootfs tagged.", "output", storeResult)

	return 0
}
