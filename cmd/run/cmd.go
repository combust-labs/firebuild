package run

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/combust-labs/firebuild/cmd"
	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/metadata"
	"github.com/combust-labs/firebuild/pkg/naming"
	"github.com/combust-labs/firebuild/pkg/storage"
	"github.com/combust-labs/firebuild/pkg/strategy"
	"github.com/combust-labs/firebuild/pkg/strategy/arbitrary"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	"github.com/docker/docker/pkg/namesgenerator"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/hashicorp/go-hclog"
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
)

func initFlags() {
	Command.Flags().AddFlagSet(cniConfig.FlagSet())
	Command.Flags().AddFlagSet(commandConfig.FlagSet())
	Command.Flags().AddFlagSet(egressTestConfig.FlagSet())
	Command.Flags().AddFlagSet(jailingFcConfig.FlagSet())
	Command.Flags().AddFlagSet(logConfig.FlagSet())
	Command.Flags().AddFlagSet(machineConfig.FlagSet())
	Command.Flags().AddFlagSet(runCache.FlagSet())
	// Storage provider flags:
	cmd.AddStorageFlags(Command.Flags())
}

func init() {
	initFlags()
}

func run(cobraCommand *cobra.Command, _ []string) {

	if commandConfig.Hostname == "" {
		commandConfig.Hostname = strings.ReplaceAll(namesgenerator.GetRandomName(0), "_", "-")
	}

	cleanup := utils.NewDefers()
	defer cleanup.CallAll()

	rootLogger := logConfig.NewLogger("run")

	storageImpl, resolveErr := cmd.GetStorageImpl(rootLogger)
	if resolveErr != nil {
		rootLogger.Error("failed resolving storage provider", "reason", resolveErr)
		os.Exit(1)
	}

	validatingConfigs := []configs.ValidatingConfig{
		commandConfig,
		jailingFcConfig,
		runCache,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			rootLogger.Error("configuration is invalid", "reason", err)
			os.Exit(1)
		}
	}

	// create cache directory:
	if err := os.MkdirAll(runCache.RunCache, 0755); err != nil {
		rootLogger.Error("failed creating run cache directory", "reason", err)
		os.Exit(1)
	}

	cacheDirectory := filepath.Join(runCache.RunCache, jailingFcConfig.VMMID())
	if err := os.Mkdir(cacheDirectory, 0755); err != nil {
		rootLogger.Error("failed creating run VMM cache directory", "reason", err)
		os.Exit(1)
	}

	cleanup.Add(func() {
		rootLogger.Info("cleaning up temp build directory")
		if err := os.RemoveAll(cacheDirectory); err != nil {
			rootLogger.Info("temp build directory removal status", "error", err)
		}
	})

	// resolve kernel:
	resolvedKernel, kernelResolveErr := storageImpl.FetchKernel(&storage.KernelLookup{
		ID: machineConfig.MachineVMLinuxID,
	})
	if kernelResolveErr != nil {
		rootLogger.Error("failed resolving kernel", "reason", kernelResolveErr)
		cleanup.CallAll() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

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
		cleanup.CallAll() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

	// the metadata here must be MDRootfs:
	mdRootfs, unwrapErr := metadata.MDRootfsFromInterface(resolvedRootfs.Metadata().(map[string]interface{}))
	if unwrapErr != nil {
		rootLogger.Error("failed unwrapping metadata", "reason", unwrapErr)
		cleanup.CallAll() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

	// we do need to copy the rootfs file to a temp directory
	// because the jailer directory indeed links to the target rootfs
	// and changes are persisted
	runRootfs := filepath.Join(cacheDirectory, naming.RootfsFileName)
	if err := utils.CopyFile(resolvedRootfs.HostPath(), runRootfs, cmd.RootFSCopyBufferSize); err != nil {
		rootLogger.Error("failed copying requested rootfs to temp build location",
			"source", resolvedRootfs.HostPath(),
			"target", runRootfs,
			"reason", err)
		cleanup.CallAll() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

	// get the veth interface name and write to also to a file:
	vethIfaceName := naming.GetRandomVethName()

	// don't use resolvedRootfs.HostPath() below this point:
	machineConfig.
		WithDaemonize(commandConfig.Daemonize).
		WithKernelOverride(resolvedKernel.HostPath()).
		WithRootfsOverride(runRootfs)

	vmmEnvironment, envErr := commandConfig.MergedEnvironment()
	if envErr != nil {
		rootLogger.Error("failed merging environment", "reason", envErr)
		cleanup.CallAll() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

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

	strategyPublicKeys := []ssh.PublicKey{}
	if commandConfig.IdentityFile != "" {
		sshPublicKey, readErr := utils.SSHPublicKeyFromFile(commandConfig.IdentityFile)
		if readErr != nil {
			rootLogger.Error("failed reading an SSH key configured with --identity-file", "reason", readErr)
			cleanup.CallAll() // manually - defers don't run on os.Exit
			os.Exit(1)
		}
		strategyPublicKeys = append(strategyPublicKeys, sshPublicKey)
	}

	strategyConfig := &strategy.PseudoCloudInitHandlerConfig{
		Chroot:         jailingFcConfig.JailerChrootDirectory(),
		RootfsFileName: filepath.Base(machineConfig.RootfsOverride()),
		SSHUser:        machineConfig.MachineSSHUser,

		// VMM settings:
		Environment: vmmEnvironment,
		Hostname:    commandConfig.Hostname,
		PublicKeys:  strategyPublicKeys,
	}

	strategy := configs.DefaultFirectackerStrategy(machineConfig).
		AddRequirements(func() *arbitrary.HandlerPlacement {
			return arbitrary.NewHandlerPlacement(strategy.
				NewPseudoCloudInitHandler(rootLogger, strategyConfig), firecracker.CreateBootSourceHandlerName)
		})

	vmmProvider := vmm.NewDefaultProvider(cniConfig, jailingFcConfig, machineConfig).
		WithHandlersAdapter(strategy).
		WithVethIfaceName(vethIfaceName)

	vmmCtx, vmmCancel := context.WithCancel(context.Background())
	cleanup.Add(func() {
		vmmCancel()
	})

	startedMachine, runErr := vmmProvider.Start(vmmCtx)
	if runErr != nil {
		vmmLogger.Error("firecracker VMM did not start, run failed", "reason", runErr)
		return
	}

	machineMetadata, metadataErr := startedMachine.Metadata()
	if metadataErr != nil {
		startedMachine.Stop(vmmCtx, nil)
		vmmLogger.Error("Failed fetching machine metadata", "reason", metadataErr)
		return
	}

	ifaceStaticConfig := machineMetadata.NetworkInterfaces[0].StaticConfiguration

	vmmLogger = vmmLogger.With("ip-address", ifaceStaticConfig.IPConfiguration.IP)

	if err := vmm.WriteMetadataToFile(cacheDirectory, machineMetadata, mdRootfs); err != nil {
		vmmLogger.Error("failed writing machine metadata to file", "reason", err, "metadata", machineMetadata)
	}

	if commandConfig.Daemonize {
		vmmLogger.Info("VMM running as a daemon",
			"ip-net", ifaceStaticConfig.IPConfiguration.IPAddr,
			"jailer-dir", jailingFcConfig.JailerChrootDirectory(),
			"cache-dir", cacheDirectory,
			"pid", machineMetadata.PID.Pid)
		os.Exit(0) // don't trigger defers
	}

	vmmLogger.Info("VMM running",
		"ip-net", ifaceStaticConfig.IPConfiguration.IPAddr,
		"jailer-dir", jailingFcConfig.JailerChrootDirectory(),
		"cache-dir", cacheDirectory,
		"pid", machineMetadata.PID.Pid)

	chanStopStatus := installSignalHandlers(context.Background(), vmmLogger, startedMachine)

	startedMachine.Wait(context.Background())
	startedMachine.Cleanup(chanStopStatus)

	vmmLogger.Info("machine is stopped", "gracefully", <-chanStopStatus)

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
