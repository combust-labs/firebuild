package run

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/combust-labs/firebuild/cmd"
	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/metadata"
	"github.com/combust-labs/firebuild/pkg/naming"
	"github.com/combust-labs/firebuild/pkg/storage"
	"github.com/combust-labs/firebuild/pkg/strategy"
	"github.com/combust-labs/firebuild/pkg/strategy/arbitrary"
	"github.com/combust-labs/firebuild/pkg/tracing"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/hashicorp/go-hclog"
	"github.com/opentracing/opentracing-go"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "run",
	Short: "Run a VMM using a pre-built file system",
	Run:   run,
	Long:  ``,
}

var (
	cniConfig        = configs.NewCNIConfig()
	commandConfig    = configs.NewRunCommandConfig()
	egressTestConfig = configs.NewEgressTestConfig()
	jailingFcConfig  = configs.NewJailingFirecrackerConfig()
	logConfig        = configs.NewLogginConfig()
	machineConfig    = configs.NewMachineConfig()
	runCache         = configs.NewRunCacheConfig()
	tracingConfig    = configs.NewTracingConfig("vmm-run")
)

func initFlags() {
	Command.Flags().AddFlagSet(cniConfig.FlagSet())
	Command.Flags().AddFlagSet(commandConfig.FlagSet())
	Command.Flags().AddFlagSet(egressTestConfig.FlagSet())
	Command.Flags().AddFlagSet(jailingFcConfig.FlagSet())
	Command.Flags().AddFlagSet(logConfig.FlagSet())
	Command.Flags().AddFlagSet(machineConfig.FlagSet())
	Command.Flags().AddFlagSet(runCache.FlagSet())
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

	if commandConfig.Hostname == "" {
		commandConfig.Hostname = utils.RandomHostname()
	}

	regularDefers := utils.NewDefers()
	defer regularDefers.CallAll()

	cleanup := utils.NewDefers()
	defer cleanup.CallAll()

	rootLogger := logConfig.NewLogger("run")

	// tracing:

	rootLogger.Info("configuring tracing", "enabled", tracingConfig.Enable, "application-name", tracingConfig.ApplicationName)

	tracer, tracerCleanupFunc, tracerErr := tracing.GetTracer(rootLogger.Named("tracer"), tracingConfig)
	if tracerErr != nil {
		rootLogger.Error("failed constructing tracer", "reason", tracerErr)
		return 1
	}

	regularDefers.Add(tracerCleanupFunc)

	spanRun := tracer.StartSpan("run")
	spanRun.SetTag("vmm-id", jailingFcConfig.VMMID())
	spanRun.SetTag("hostname", commandConfig.Hostname)
	defer spanRun.Finish()

	// storage:
	storageImpl, resolveErr := cmd.GetStorageImpl(rootLogger)
	if resolveErr != nil {
		rootLogger.Error("failed resolving storage provider", "reason", resolveErr)
		return 1
	}

	validatingConfigs := []configs.ValidatingConfig{
		commandConfig,
		jailingFcConfig,
		runCache,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			rootLogger.Error("configuration is invalid", "reason", err)
			return 1
		}
	}

	spanCacheCreate := tracer.StartSpan("create-cache-dir", opentracing.ChildOf(spanRun.Context()))

	// create cache directory:
	if err := os.MkdirAll(runCache.RunCache, 0755); err != nil {
		rootLogger.Error("failed creating run cache directory", "reason", err)
		return 1
	}

	cacheDirectory := filepath.Join(runCache.RunCache, jailingFcConfig.VMMID())
	if err := os.Mkdir(cacheDirectory, 0755); err != nil {
		rootLogger.Error("failed creating run VMM cache directory", "reason", err)
		return 1
	}

	cleanup.Add(func() {
		rootLogger.Info("cleaning up temp build directory")
		if err := os.RemoveAll(cacheDirectory); err != nil {
			rootLogger.Info("temp build directory removal status", "error", err)
		}
	})

	spanCacheCreate.Finish()

	spanResolveKernel := tracer.StartSpan("resolve-kernel", opentracing.ChildOf(spanCacheCreate.Context()))

	// resolve kernel:
	resolvedKernel, kernelResolveErr := storageImpl.FetchKernel(&storage.KernelLookup{
		ID: machineConfig.MachineVMLinuxID,
	})
	if kernelResolveErr != nil {
		rootLogger.Error("failed resolving kernel", "reason", kernelResolveErr)
		return 1
	}

	spanResolveKernel.Finish()

	spanResolveRootfs := tracer.StartSpan("resolve-rootfs", opentracing.ChildOf(spanResolveKernel.Context()))

	// resolve rootfs:
	from := commands.From{BaseImage: commandConfig.From}
	structuredFrom := from.ToStructuredFrom()
	resolvedRootfs, rootfsResolveErr := storageImpl.FetchRootfs(&storage.RootfsLookup{
		Org:     structuredFrom.Org(),
		Image:   structuredFrom.Image(),
		Version: structuredFrom.Version(),
	})
	if rootfsResolveErr != nil {
		rootLogger.Error("failed resolving rootfs", "reason", rootfsResolveErr)
		return 1
	}

	spanResolveRootfs.Finish()

	spanRootfsMetadata := tracer.StartSpan("rootfs-metadata", opentracing.ChildOf(spanResolveRootfs.Context()))

	// the metadata here must be MDRootfs:
	mdRootfs, unwrapErr := metadata.MDRootfsFromInterface(resolvedRootfs.Metadata().(map[string]interface{}))
	if unwrapErr != nil {
		rootLogger.Error("failed unwrapping metadata", "reason", unwrapErr)
		return 1
	}

	spanRootfsMetadata.Finish()

	spanRootfsCopy := tracer.StartSpan("rootfs-copy", opentracing.ChildOf(spanRootfsMetadata.Context()))

	// we do need to copy the rootfs file to a temp directory
	// because the jailer directory indeed links to the target rootfs
	// and changes are persisted
	runRootfs := filepath.Join(cacheDirectory, naming.RootfsFileName)
	if err := utils.CopyFile(resolvedRootfs.HostPath(), runRootfs, cmd.RootFSCopyBufferSize); err != nil {
		rootLogger.Error("failed copying requested rootfs to temp build location",
			"source", resolvedRootfs.HostPath(),
			"target", runRootfs,
			"reason", err)
		return 1
	}

	spanRootfsCopy.Finish()

	// get the veth interface name and write to also to a file:
	vethIfaceName := naming.GetRandomVethName()
	spanRun.SetTag("ifname", vethIfaceName)

	// don't use resolvedRootfs.HostPath() below this point:
	machineConfig.
		WithDaemonize(commandConfig.Daemonize).
		WithKernelOverride(resolvedKernel.HostPath()).
		WithRootfsOverride(runRootfs)

	vmmLogger := rootLogger.With("vmm-id", jailingFcConfig.VMMID(), "veth-name", vethIfaceName)

	vmmLogger.Info("running VMM",
		"from", commandConfig.From,
		"source-rootfs", machineConfig.RootfsOverride(),
		"jail", jailingFcConfig.JailerChrootDirectory())

	cleanup.Add(func() {
		vmmLogger.Info("cleaning up jail directory")
		if err := os.RemoveAll(jailingFcConfig.JailerChrootDirectory()); err != nil {
			vmmLogger.Info("jail directory removal status", "error", err)
		}
	})

	// gather the running vmm metadata:
	runMetadata := &metadata.MDRun{
		Configs: metadata.MDRunConfigs{
			CNI:     cniConfig,
			Jailer:  jailingFcConfig,
			Machine: machineConfig,
		},
		Hostname:     commandConfig.Hostname,
		IdentityFile: commandConfig.IdentityFile,
		Rootfs:       mdRootfs,
		RunCache:     cacheDirectory,
		Type:         metadata.MetadataTypeRun,
	}

	vmmStrategy := configs.DefaultFirectackerStrategy(machineConfig)

	if !commandConfig.DisablePseudoCloudInit {
		vmmEnvironment, envErr := commandConfig.MergedEnvironment()
		if envErr != nil {
			rootLogger.Error("failed merging environment", "reason", envErr)
			return 1
		}
		strategyPublicKeys := []ssh.PublicKey{}
		if commandConfig.IdentityFile != "" {
			sshPublicKey, readErr := utils.SSHPublicKeyFromFile(commandConfig.IdentityFile)
			if readErr != nil {
				rootLogger.Error("failed reading an SSH key configured with --identity-file", "reason", readErr)
				return 1
			}
			strategyPublicKeys = append(strategyPublicKeys, sshPublicKey)
		}
		strategyConfig := &strategy.PseudoCloudInitHandlerConfig{
			Chroot:         jailingFcConfig.JailerChrootDirectory(),
			RootfsFileName: filepath.Base(machineConfig.RootfsOverride()),
			SSHUser:        machineConfig.MachineSSHUser,
			Metadata:       runMetadata,
			// VMM settings:
			Environment: vmmEnvironment,
			Hostname:    commandConfig.Hostname,
			PublicKeys:  strategyPublicKeys,

			SpanContext: spanRootfsCopy.Context(),
			Tracer:      tracer,
		}
		vmmStrategy = vmmStrategy.AddRequirements(func() *arbitrary.HandlerPlacement {
			return arbitrary.NewHandlerPlacement(strategy.
				NewPseudoCloudInitHandler(rootLogger, strategyConfig), firecracker.CreateBootSourceHandlerName)
		})
	}

	vmmStrategy = vmmStrategy.AddRequirements(func() *arbitrary.HandlerPlacement {
		// add this one after the previous one so by he logic,
		// this one will be placed and executed before the first one
		return arbitrary.NewHandlerPlacement(strategy.
			NewMetadataExtractorHandler(rootLogger, runMetadata), firecracker.CreateBootSourceHandlerName)
	})

	spanVMMCreate := tracer.StartSpan("vmm-create", opentracing.ChildOf(spanRootfsCopy.Context()))

	vmmProvider := vmm.NewDefaultProvider(cniConfig, jailingFcConfig, machineConfig).
		WithHandlersAdapter(vmmStrategy).
		WithVethIfaceName(vethIfaceName)

	vmmCtx, vmmCancel := context.WithCancel(context.Background())
	cleanup.Add(func() {
		vmmCancel()
	})

	spanVMMCreate.Finish()

	spanVMMStart := tracer.StartSpan("vmm-start", opentracing.ChildOf(spanVMMCreate.Context()))

	startedMachine, runErr := vmmProvider.Start(vmmCtx)
	if runErr != nil {
		vmmLogger.Error("firecracker VMM did not start, run failed", "reason", runErr)
		return 1
	}

	spanVMMStart.Finish()

	metadataErr := startedMachine.DecorateMetadata(runMetadata)
	if metadataErr != nil {
		startedMachine.Stop(vmmCtx, nil)
		vmmLogger.Error("Failed fetching machine metadata", "reason", metadataErr)
		return 1
	}

	vmmLogger = vmmLogger.With("ip-address", runMetadata.NetworkInterfaces[0].StaticConfiguration.IPConfiguration.IP)
	spanRun.SetTag("ip", runMetadata.NetworkInterfaces[0].StaticConfiguration.IPConfiguration.IP)

	spanVMMStarted := tracer.StartSpan("vmm-started", opentracing.ChildOf(spanVMMStart.Context()))

	if err := vmm.WriteMetadataToFile(runMetadata); err != nil {
		vmmLogger.Error("failed writing machine metadata to file", "reason", err, "metadata", runMetadata)
	}

	spanVMMStarted.Finish()

	if commandConfig.Daemonize {
		vmmLogger.Info("VMM running as a daemon",
			"jailer-dir", jailingFcConfig.JailerChrootDirectory(),
			"cache-dir", cacheDirectory)
		cleanup.Trigger(false) // do not trigger cleanup defers
		return 0
	}

	vmmLogger.Info("VMM running",
		"jailer-dir", jailingFcConfig.JailerChrootDirectory(),
		"cache-dir", cacheDirectory)

	chanStopStatus := installSignalHandlers(context.Background(), vmmLogger, startedMachine)

	startedMachine.Wait(context.Background())
	startedMachine.Cleanup(chanStopStatus)

	vmmLogger.Info("machine is stopped", "gracefully", <-chanStopStatus)

	return 0

}

func installSignalHandlers(ctx context.Context, logger hclog.Logger, m vmm.StartedMachine) chan bool {
	chanStopped := make(chan bool, 1)
	go func() {
		// Clear selected default handlers installed by the firecracker SDK:
		signal.Reset(os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		for {
			switch s := <-c; {
			case s == syscall.SIGTERM || s == os.Interrupt:
				logger.Info("Caught SIGINT, requesting clean shutdown")
				chanStopped <- m.Stop(ctx, nil)
			}
		}
	}()
	return chanStopped
}
