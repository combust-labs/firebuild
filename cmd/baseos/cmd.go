package baseos

import (
	"context"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild/build/commands"
	"github.com/combust-labs/firebuild/build/reader"
	"github.com/combust-labs/firebuild/build/stage"
	"github.com/combust-labs/firebuild/cmd"
	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/containers"
	"github.com/combust-labs/firebuild/pkg/naming"
	"github.com/combust-labs/firebuild/pkg/storage"
	"github.com/combust-labs/firebuild/pkg/utils"
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
)

func initFlags() {
	Command.Flags().AddFlagSet(commandConfig.FlagSet())
	Command.Flags().AddFlagSet(logConfig.FlagSet())
	// Storage provider flags:
	cmd.AddStorageFlags(Command.Flags())
}

func init() {
	initFlags()
}

func run(cobraCommand *cobra.Command, _ []string) {

	rootLogger := logConfig.NewLogger("baseos")

	dockerStat, statErr := os.Stat(commandConfig.Dockerfile)
	if statErr != nil {
		rootLogger.Error("error while resolving --dockerfile path", "reason", statErr)
		os.Exit(1)
	}
	if dockerStat.IsDir() {
		rootLogger.Error("--dockerfile points at a directory")
		os.Exit(1)
	}

	storageImpl, resolveErr := cmd.GetStorageImpl(rootLogger)
	if resolveErr != nil {
		rootLogger.Error("failed resolving storage provider", "reason", resolveErr)
		os.Exit(1)
	}

	tempDirectory, err := ioutil.TempDir("", "")
	if err != nil {
		rootLogger.Error("failed creating temporary build directory", "reason", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tempDirectory)

	// we parse the file to establish the base operating system we build
	// we must enforce some constants so we assume here
	// no multi-stage builds - only main stage
	readResults, err := reader.ReadFromString(commandConfig.Dockerfile, tempDirectory)
	if err != nil {
		rootLogger.Error("failed parsing Dockerfile", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	scs, errs := stage.ReadStages(readResults.Commands())
	for _, err := range errs {
		rootLogger.Warn("stages read contained an error", "reason", err)
	}

	if len(scs.Unnamed()) != 1 {
		rootLogger.Error("Dockerfile must contain exactly one unnamed FROM build stage")
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	if len(scs.Named()) > 0 {
		rootLogger.Error("Dockerfile contains other named stages, this is not supported")
		os.RemoveAll(tempDirectory)
		os.Exit(1)
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
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	rootLogger.Info("building base operating system root file system", "os", fromToBuild.BaseImage)

	// we have to build the Docker image, we can use the dependency builder here:
	client, clientErr := containers.GetDefaultClient()
	if clientErr != nil {
		rootLogger.Error("failed creating a Docker client")
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	tagName := strings.ToLower(utils.RandStringBytes(32)) + ":build"

	rootLogger.Info("building base operating system Docker image", "os", fromToBuild.BaseImage)

	if err := containers.ImageBuild(context.Background(), client, rootLogger,
		filepath.Dir(commandConfig.Dockerfile), "Dockerfile", tagName); err != nil {
		rootLogger.Error("failed building base OS Docker image", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	if _, findErr := containers.FindImageIDByTag(context.Background(), client, tagName); findErr != nil {
		// be extra careful:
		rootLogger.Error("expected docker image not found", "reason", findErr)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	rootLogger.Info("image ready, creating EXT4 root file system file", "os", fromToBuild.BaseImage)

	rootFSFile := filepath.Join(tempDirectory, naming.RootfsFileName)

	if err := utils.CreateRootFSFile(rootFSFile, commandConfig.FSSizeMBs); err != nil {
		rootLogger.Error("failed creating rootfs file", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	rootLogger.Info("EXT4 file created, making file system", "path", rootFSFile, "size-mb", commandConfig.FSSizeMBs)

	if err := utils.MkfsExt4(rootFSFile); err != nil {
		rootLogger.Error("failed creating EXT4 in rootfs file", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	rootLogger.Info("EXT4 file system created, mouting", "path", rootFSFile, "size-mb", commandConfig.FSSizeMBs)

	// create the mount directory:
	mountDir := filepath.Join(tempDirectory, "mount")
	mkdirErr := os.Mkdir(mountDir, fs.ModePerm)
	if mkdirErr != nil {
		rootLogger.Error("failed creating EXT4 mount directory", "reason", mkdirErr)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	if err := utils.Mount(rootFSFile, mountDir); err != nil {
		rootLogger.Error("failed mounting rootfs file in mount dir", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	rootLogger.Info("EXT4 file mounted in mount dir", "rootfs", rootFSFile, "mount-dir", mountDir)

	if err := containers.ImageBaseOSExport(context.Background(), client, rootLogger, mountDir, tagName); err != nil {
		rootLogger.Error("failed building root file system for the base OS", "reason", err)
		// continue to clean up
	}

	if err := containers.ImageRemove(context.Background(), client, rootLogger, tagName); err != nil {
		rootLogger.Error("failed post-build image clean up", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	if err := utils.Umount(mountDir); err != nil {
		rootLogger.Error("failed unmounting rootfs mount dir", "reason", err)
		os.RemoveAll(tempDirectory)
		os.Exit(1)
	}

	rootLogger.Info("EXT4 file unmounted from mount dir", "rootfs", rootFSFile, "mount-dir", mountDir)

	structuredBase := fromToBuild.ToStructuredFrom()

	storeResult, storeErr := storageImpl.StoreRootfsFile(&storage.RootfsStore{
		LocalPath: rootFSFile,
		Metadata:  map[string]interface{}{},
		Org:       structuredBase.Org(),
		Image:     structuredBase.OS(),
		Version:   structuredBase.Version(),
	})
	if storeErr != nil {
		rootLogger.Error("failed storing built rootfs", "reason", storeErr)
		return
	}

	rootLogger.Info("Build completed successfully. Rootfs tagged.", "output", storeResult)

}
