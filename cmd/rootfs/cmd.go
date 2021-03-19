package rootfs

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/build"
	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/build/reader"
	"github.com/combust-labs/firebuild/pkg/build/resources"
	"github.com/combust-labs/firebuild/pkg/build/stage"
	"github.com/combust-labs/firebuild/pkg/metadata"
	"github.com/combust-labs/firebuild/pkg/naming"
	"github.com/combust-labs/firebuild/pkg/profiles"
	"github.com/combust-labs/firebuild/pkg/remote"
	"github.com/combust-labs/firebuild/pkg/storage"
	"github.com/combust-labs/firebuild/pkg/storage/resolver"
	"github.com/combust-labs/firebuild/pkg/tracing"
	"github.com/opentracing/opentracing-go"

	"github.com/combust-labs/firebuild/pkg/strategy"
	"github.com/combust-labs/firebuild/pkg/strategy/arbitrary"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "rootfs",
	Short: "Build A VMM root file system from a Docker image",
	Run:   run,
	Long:  ``,
}

var (
	cniConfig        = configs.NewCNIConfig()
	commandConfig    = configs.NewRootfsCommandConfig()
	egressTestConfig = configs.NewEgressTestConfig()
	jailingFcConfig  = configs.NewJailingFirecrackerConfig()
	logConfig        = configs.NewLogginConfig()
	machineConfig    = configs.NewMachineConfig()
	profilesConfig   = configs.NewProfileCommandConfig()
	runCache         = configs.NewRunCacheConfig()
	tracingConfig    = configs.NewTracingConfig("firebuild-rootfs")
	rsaKeySize       = 4096

	storageResolver = resolver.NewDefaultResolver()
)

func initFlags() {
	Command.Flags().AddFlagSet(cniConfig.FlagSet())
	Command.Flags().AddFlagSet(commandConfig.FlagSet())
	Command.Flags().AddFlagSet(egressTestConfig.FlagSet())
	Command.Flags().AddFlagSet(jailingFcConfig.FlagSet())
	Command.Flags().AddFlagSet(logConfig.FlagSet())
	Command.Flags().AddFlagSet(machineConfig.FlagSet())
	Command.Flags().AddFlagSet(profilesConfig.FlagSet())
	Command.Flags().AddFlagSet(runCache.FlagSet())
	Command.Flags().AddFlagSet(tracingConfig.FlagSet())
	// Storage provider flags:
	resolver.AddStorageFlags(Command.Flags())
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

	rootLogger := logConfig.NewLogger("rootfs")

	if profilesConfig.Profile != "" {
		profile, err := profiles.ReadProfile(profilesConfig.Profile, profilesConfig.ProfileConfDir)
		if err != nil {
			rootLogger.Error("failed resolving profile", "reason", err, "profile", profilesConfig.Profile)
			return 1
		}
		if err := profile.UpdateConfigs(jailingFcConfig, runCache, tracingConfig); err != nil {
			rootLogger.Error("error updating configuration from profile", "reason", err)
			return 1
		}
		storageResolver.
			WithConfigurationOverride(profile.GetMergedStorageConfig()).
			WithTypeOverride(profile.Profile().StorageProvider)
	}

	// tracing:

	rootLogger.Info("configuring tracing", "enabled", tracingConfig.Enable, "application-name", tracingConfig.ApplicationName)

	tracer, tracerCleanupFunc, tracerErr := tracing.GetTracer(rootLogger.Named("tracer"), tracingConfig)
	if tracerErr != nil {
		rootLogger.Error("failed constructing tracer", "reason", tracerErr)
		return 1
	}

	cleanup.Add(tracerCleanupFunc)

	rootLogger, spanBuild := tracing.ApplyTraceLogDiscovery(rootLogger, tracer.StartSpan("build-rootfs"))
	cleanup.Add(func() {
		spanBuild.Finish()
	})

	storageImpl, resolveErr := storageResolver.GetStorageImpl(rootLogger)
	if resolveErr != nil {
		rootLogger.Error("failed resolving storage provider", "reason", resolveErr)
		spanBuild.SetBaggageItem("error", resolveErr.Error())
		return 1
	}

	validatingConfigs := []configs.ValidatingConfig{
		jailingFcConfig,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			rootLogger.Error("Configuration is invalid", "reason", err)
			spanBuild.SetBaggageItem("error", err.Error())
			return 1
		}
	}

	if commandConfig.Tag == "" {
		rootLogger.Error("--tag is required")
		spanBuild.SetBaggageItem("error", "--tag is required")
		return 1
	}

	if !utils.IsValidTag(commandConfig.Tag) {
		rootLogger.Error("--tag value is invalid", "tag", commandConfig.Tag)
		spanBuild.SetBaggageItem("error", fmt.Errorf("--tag value is invalid: '%s'", commandConfig.Tag).Error())
		return 1
	}

	spanTempDir := tracer.StartSpan("rootfs-temp-dir", opentracing.ChildOf(spanBuild.Context()))

	// create cache directory:
	cacheDirectory := filepath.Join(runCache.LocationBuilds(), jailingFcConfig.VMMID())
	if err := os.MkdirAll(cacheDirectory, 0755); err != nil {
		rootLogger.Error("failed creating build VMM cache directory", "reason", err)
		spanTempDir.SetBaggageItem("error", err.Error())
		spanTempDir.Finish()
		return 1
	}

	cleanup.Add(func() {
		span := tracer.StartSpan("rootfs-temp-cleanup", opentracing.ChildOf(spanTempDir.Context()))
		rootLogger.Info("cleaning up temp build directory")
		if err := os.RemoveAll(cacheDirectory); err != nil {
			rootLogger.Info("temp build directory removal status", "error", err)
			span.SetBaggageItem("error", err.Error())
		}
		span.Finish()
	})

	spanTempDir.Finish()

	spanGenerateKeys := tracer.StartSpan("rootfs-generate-keys", opentracing.ChildOf(spanTempDir.Context()))

	rsaPrivateKey, rsaPrivateKeyErr := utils.GenerateRSAPrivateKey(rsaKeySize)
	if rsaPrivateKeyErr != nil {
		rootLogger.Error("failed generating private key for the VMM build", "reason", rsaPrivateKeyErr)
		spanGenerateKeys.SetBaggageItem("error", rsaPrivateKeyErr.Error())
		spanGenerateKeys.Finish()
		return 1
	}
	sshPublicKey, sshPublicKeyErr := utils.GetSSHKey(rsaPrivateKey)
	if sshPublicKeyErr != nil {
		rootLogger.Error("failed generating public SSH key from the private RSA key", "reason", sshPublicKeyErr)
		spanGenerateKeys.SetBaggageItem("error", sshPublicKeyErr.Error())
		spanGenerateKeys.Finish()
		return 1
	}

	spanGenerateKeys.Finish()

	// -- Command specific:

	spanParseDockerfile := tracer.StartSpan("rootfs-parse-dockerfile", opentracing.ChildOf(spanGenerateKeys.Context()))

	readResults, err := reader.ReadFromString(commandConfig.Dockerfile, cacheDirectory)
	if err != nil {
		rootLogger.Error("failed parsing Dockerfile", "reason", err)
		spanParseDockerfile.SetBaggageItem("error", err.Error())
		spanParseDockerfile.Finish()
		return 1
	}

	spanParseDockerfile.Finish()

	spanReadStages := tracer.StartSpan("rootfs-read-stages", opentracing.ChildOf(spanParseDockerfile.Context()))

	scs, errs := stage.ReadStages(readResults.Commands())
	for _, err := range errs {
		rootLogger.Warn("stages read contained an error", "reason", err)
	}

	unnamed := scs.Unnamed()
	if len(unnamed) != 1 {
		rootLogger.Error("expected exactly one unnamed build stage but found", "num-unnamed", len(unnamed))
		spanReadStages.SetBaggageItem("error", "unnamed stages count must be 1")
		spanReadStages.Finish()
		return 1
	}

	mainStage := unnamed[0]
	if !mainStage.IsValid() {
		rootLogger.Error("main build stage invalid: no base to build from")
		spanReadStages.SetBaggageItem("error", "main build stage is invalid")
		spanReadStages.Finish()
		return 1
	}

	spanReadStages.Finish()

	spanBuildContext := tracer.StartSpan("rootfs-build-context", opentracing.ChildOf(spanReadStages.Context()))

	// The first thing to do is to resolve the Dockerfile:
	buildContext := build.NewDefaultBuild()
	if err := buildContext.AddInstructions(unnamed[0].Commands()...); err != nil {
		rootLogger.Error("commands could not be processed", "reason", err)
		spanBuildContext.SetBaggageItem("error", err.Error())
		spanBuildContext.Finish()
		return 1
	}

	structuredFrom := buildContext.From().ToStructuredFrom()

	// which resources from dependencies do we need:
	requiredCopies := []commands.Copy{}
	for _, stage := range scs.All() {
		for _, stageCommand := range stage.Commands() {
			switch tcommand := stageCommand.(type) {
			case commands.Copy:
				if tcommand.Stage != "" {
					requiredCopies = append(requiredCopies, tcommand)
				}
			}
		}
	}

	// resolve dependencies:
	dependencyResources := map[string][]resources.ResolvedResource{}
	for _, stage := range scs.All() {
		for _, dependency := range stage.DependsOn() {
			if _, ok := dependencyResources[dependency]; !ok {
				dependencyStage := scs.NamedStage(dependency)
				if dependencyStage == nil {
					rootLogger.Error("main build stage depends on non-existent stage", "dependency", dependency)
					spanBuildContext.SetBaggageItem("error", "depends on non-existing stage")
					spanBuildContext.Finish()
					return 1
				}
				spanDependencyBuild := tracer.StartSpan("rootfs-build-dependency", opentracing.ChildOf(spanBuildContext.Context()))
				dependencyBuilder := build.NewDefaultDependencyBuild(dependencyStage, cacheDirectory, filepath.Join(cacheDirectory, "sources"))
				resolvedResources, buildError := dependencyBuilder.Build(requiredCopies)
				if buildError != nil {
					rootLogger.Error("failed building stage dependency", "stage", stage.Name(), "dependency", dependency, "reason", buildError)
					spanDependencyBuild.SetBaggageItem("error", buildError.Error())
					spanDependencyBuild.Finish()
					spanBuildContext.Finish()
					return 1
				}
				dependencyResources[dependency] = resolvedResources
				spanDependencyBuild.Finish()
			}
		}
	}

	spanBuildContext.Finish()

	// -- Command specific // END

	spanResolveKernel := tracer.StartSpan("rootfs-resolve-kernel", opentracing.ChildOf(spanBuildContext.Context()))

	// resolve kernel:
	resolvedKernel, kernelResolveErr := storageImpl.FetchKernel(&storage.KernelLookup{
		ID: machineConfig.MachineVMLinuxID,
	})
	if kernelResolveErr != nil {
		rootLogger.Error("failed resolving kernel", "reason", kernelResolveErr)
		spanResolveKernel.SetBaggageItem("error", kernelResolveErr.Error())
		spanResolveKernel.Finish()
		return 1
	}

	rootLogger.Info("kernel resolved", "host-path", resolvedKernel.HostPath())

	spanResolveKernel.Finish()

	spanResolveRootfs := tracer.StartSpan("rootfs-resolve-rootfs", opentracing.ChildOf(spanResolveKernel.Context()))

	// resolve rootfs:
	resolvedRootfs, rootfsResolveErr := storageImpl.FetchRootfs(&storage.RootfsLookup{
		Org:     structuredFrom.Org(),
		Image:   structuredFrom.Image(),
		Version: structuredFrom.Version(),
	})
	if rootfsResolveErr != nil {
		rootLogger.Error("failed resolving rootfs", "reason", rootfsResolveErr)
		spanResolveRootfs.SetBaggageItem("error", rootfsResolveErr.Error())
		spanResolveRootfs.Finish()
		return 1
	}

	rootLogger.Info("rootfs resolved", "host-path", resolvedRootfs.HostPath())

	spanResolveRootfs.Finish()

	spanRootfsCopy := tracer.StartSpan("rootfs-copy", opentracing.ChildOf(spanResolveRootfs.Context()))

	// we do need to copy the rootfs file to a temp directory
	// because the jailer directory indeed links to the target rootfs
	// and changes are persisted
	buildRootfs := filepath.Join(cacheDirectory, naming.RootfsFileName)
	if err := utils.CopyFile(resolvedRootfs.HostPath(), buildRootfs, utils.RootFSCopyBufferSize); err != nil {
		rootLogger.Error("failed copying requested rootfs to temp build location",
			"source", resolvedRootfs.HostPath(),
			"target", buildRootfs,
			"reason", err)
		spanRootfsCopy.SetBaggageItem("error", err.Error())
		spanRootfsCopy.Finish()
		return 1
	}

	spanRootfsCopy.Finish()

	// don't use resolvedRootfs.HostPath() below this point:
	machineConfig.
		WithKernelOverride(resolvedKernel.HostPath()).
		WithRootfsOverride(buildRootfs)

	vethIfaceName := naming.GetRandomVethName()

	vmmLogger := rootLogger.With("vmm-id", jailingFcConfig.VMMID(), "veth-name", vethIfaceName)

	vmmLogger.Info("buildiing VMM",
		"dockerfile", commandConfig.Dockerfile,
		"kernel-path", resolvedKernel.HostPath(),
		"source-rootfs", machineConfig.RootfsOverride(),
		"jail", jailingFcConfig.JailerChrootDirectory())

	cleanup.Add(func() {
		span := tracer.StartSpan("rootfs-cleanup-temp", opentracing.ChildOf(spanBuild.Context()))
		vmmLogger.Info("cleaning up jail directory")
		//if err := os.RemoveAll(jailingFcConfig.JailerChrootDirectory()); err != nil {
		//	vmmLogger.Info("jail directory removal status", "error", err)
		//	span.SetBaggageItem("error", err.Error())
		//}
		span.Finish()
	})

	// gather the running vmm metadata:
	runMetadata := &metadata.MDRun{
		Type: metadata.MetadataTypeRun,
	}

	strategyConfig := &strategy.PseudoCloudInitHandlerConfig{
		Chroot:         jailingFcConfig.JailerChrootDirectory(),
		RootfsFileName: filepath.Base(machineConfig.RootfsOverride()),
		SSHUser:        machineConfig.MachineSSHUser,
		PublicKeys: []ssh.PublicKey{
			sshPublicKey,
		},
		Tracer:      tracer,
		SpanContext: spanRootfsCopy.Context(),
	}

	strategy := configs.DefaultFirectackerStrategy(machineConfig).
		AddRequirements(func() *arbitrary.HandlerPlacement {
			return arbitrary.NewHandlerPlacement(strategy.
				NewPseudoCloudInitHandler(rootLogger, strategyConfig), firecracker.CreateBootSourceHandlerName)
		}).
		AddRequirements(func() *arbitrary.HandlerPlacement {
			// add this one after the previous one so by he logic,
			// this one will be placed and executed before the first one
			return arbitrary.NewHandlerPlacement(strategy.
				NewMetadataExtractorHandler(rootLogger, runMetadata), firecracker.CreateBootSourceHandlerName)
		})

	spanVMMCreate := tracer.StartSpan("rootfs-vmm-create", opentracing.ChildOf(spanRootfsCopy.Context()))

	vmmProvider := vmm.NewDefaultProvider(cniConfig, jailingFcConfig, machineConfig).
		WithHandlersAdapter(strategy).
		WithVethIfaceName(vethIfaceName)

	vmmCtx, vmmCancel := context.WithCancel(context.Background())
	cleanup.Add(func() {
		vmmCancel()
	})

	spanVMMCreate.Finish()

	spanVMMStart := tracer.StartSpan("rootfs-vmm-start", opentracing.ChildOf(spanVMMCreate.Context()))

	startedMachine, runErr := vmmProvider.Start(vmmCtx)
	if runErr != nil {
		vmmLogger.Error("Firecracker VMM did not start, build failed", "reason", runErr)
		spanVMMStart.SetBaggageItem("error", runErr.Error())
		spanVMMStart.Finish()
		return 1
	}

	spanVMMStart.Finish()

	ipAddress := runMetadata.NetworkInterfaces[0].StaticConfiguration.IPConfiguration.IP

	vmmLogger = vmmLogger.With("ip-address", ipAddress)

	vmmLogger.Info("VMM running")

	spanRemoteConnect := tracer.StartSpan("rootfs-remote-connect", opentracing.ChildOf(spanVMMStart.Context()))

	remoteClient, remoteErr := remote.Connect(context.Background(), remote.ConnectConfig{
		SSHPrivateKey:      *rsaPrivateKey,
		SSHUsername:        machineConfig.MachineSSHUser,
		IP:                 net.ParseIP(ipAddress),
		Port:               machineConfig.MachineSSHPort,
		EnableAgentForward: machineConfig.MachineSSHEnableAgentForward,
	}, vmmLogger.Named("remote-client"))

	if remoteErr != nil {
		startedMachine.Stop(vmmCtx, nil)
		vmmLogger.Error("Failed connecting to remote", "reason", remoteErr)
		spanRemoteConnect.SetBaggageItem("error", remoteErr.Error())
		spanRemoteConnect.Finish()
		return 1
	}

	spanRemoteConnect.Finish()

	vmmLogger.Info("Connected via SSH")

	if !egressTestConfig.EgressNoWait {
		spanEgressWait := tracer.StartSpan("rootfs-egress-wait", opentracing.ChildOf(spanRemoteConnect.Context()))
		egressCtx, egressWaitCancelFunc := context.WithTimeout(vmmCtx, time.Second*time.Duration(egressTestConfig.EgressTestTimeoutSeconds))
		defer egressWaitCancelFunc()
		if err := remoteClient.WaitForEgress(egressCtx, egressTestConfig.EgressTestTarget); err != nil {
			startedMachine.Stop(vmmCtx, remoteClient)
			vmmLogger.Error("Did not get egress connectivity within a timeout", "reason", err)
			spanEgressWait.SetBaggageItem("error", err.Error())
			spanEgressWait.Finish()
			return 1
		}
		spanEgressWait.Finish()
	} else {
		vmmLogger.Debug("Egress test explicitly skipped")
	}

	postBuildCommands := []commands.Run{}
	for _, cmd := range commandConfig.PostBuildCommands {
		postBuildCommands = append(postBuildCommands, commands.RunWithDefaults(cmd))
	}
	preBuildCommands := []commands.Run{}
	for _, cmd := range commandConfig.PreBuildCommands {
		preBuildCommands = append(preBuildCommands, commands.RunWithDefaults(cmd))
	}

	spanBuildExec := tracer.StartSpan("rootfs-build-exec", opentracing.ChildOf(spanRemoteConnect.Context()))

	if buildErr := buildContext.
		WithLogger(vmmLogger.Named("builder")).
		WithServiceInstaller(commandConfig.ServiceFileInstaller).
		WithPostBuildCommands(postBuildCommands...).
		WithPreBuildCommands(preBuildCommands...).
		WithDependencyResources(dependencyResources).
		Build(remoteClient); err != nil {
		startedMachine.Stop(vmmCtx, remoteClient)
		vmmLogger.Error("Failed boostrapping remote via SSH", "reason", buildErr)
		spanBuildExec.SetBaggageItem("error", buildErr.Error())
		spanBuildExec.Finish()
		return 1
	}

	spanBuildExec.Finish()

	spanStop := tracer.StartSpan("rootfs-vmm-stop", opentracing.ChildOf(spanBuildExec.Context()))

	startedMachine.StopAndWait(vmmCtx, remoteClient)

	spanStop.Finish()

	vmmLogger.Info("Machine is stopped. Persisting the file system...")

	spanPersist := tracer.StartSpan("rootfs-persist", opentracing.ChildOf(spanStop.Context()))

	ok, org, name, version := utils.TagDecompose(commandConfig.Tag)
	if !ok {
		vmmLogger.Error("Tag could not be decomposed", "tag", commandConfig.Tag)
		spanPersist.SetBaggageItem("error", "--tag could not be decomposed")
		spanPersist.Finish()
		return 1
	}

	fsFileName := filepath.Base(machineConfig.RootfsOverride())
	createdRootfsFile := filepath.Join(jailingFcConfig.JailerChrootDirectory(), "root", fsFileName)
	storeResult, storeErr := storageImpl.StoreRootfsFile(&storage.RootfsStore{
		LocalPath: createdRootfsFile,
		Metadata: metadata.MDRootfs{
			BuildConfig: metadata.MDRootfsConfig{
				BuildArgs:         commandConfig.BuildArgs,
				Dockerfile:        commandConfig.Dockerfile,
				PreBuildCommands:  commandConfig.PreBuildCommands,
				PostBuildCommands: commandConfig.PostBuildCommands,
			},
			CreatedAtUTC: time.Now().UTC().Unix(),
			Image: metadata.MDImage{
				Org:     org,
				Image:   name,
				Version: version,
			},
			Labels:  buildContext.Metadata(),
			Parent:  resolvedRootfs.Metadata(),
			Ports:   buildContext.ExposedPorts(),
			Tag:     commandConfig.Tag,
			Type:    metadata.MetadataTypeRootfs,
			Volumes: buildContext.Volumes(),
		},
		Org:     org,
		Image:   name,
		Version: version,
	})

	if storeErr != nil {
		vmmLogger.Error("failed storing built rootfs", "reason", storeErr)
		spanPersist.SetBaggageItem("error", storeErr.Error())
		spanPersist.Finish()
		return 1
	}

	spanPersist.Finish()

	vmmLogger.Info("Build completed successfully. Rootfs tagged.", "output", storeResult)

	return 0

}
