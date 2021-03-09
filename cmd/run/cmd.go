package run

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/combust-labs/firebuild/build/commands"
	"github.com/combust-labs/firebuild/cmd"
	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/naming"
	"github.com/combust-labs/firebuild/pkg/storage"
	"github.com/combust-labs/firebuild/pkg/strategy"
	"github.com/combust-labs/firebuild/pkg/strategy/arbitrary"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	"github.com/combust-labs/firebuild/pkg/vmm/pid"
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
	resolveKernel, kernelResolveErr := storageImpl.FetchKernel(&storage.KernelLookup{
		ID: machineConfig.MachineVMLinuxID,
	})
	if kernelResolveErr != nil {
		rootLogger.Error("failed resolving kernel", "reason", kernelResolveErr)
		os.Exit(1)
	}

	// resolve rootfs:
	from := commands.From{BaseImage: commandConfig.From}
	structuredFrom := from.ToStructuredFrom()
	resolvedRootfs, rootfsResolveErr := storageImpl.FetchRootfs(&storage.RootfsLookup{
		Org:     structuredFrom.Org(),
		Image:   structuredFrom.OS(),
		Version: structuredFrom.Version(),
	})
	if rootfsResolveErr != nil {
		rootLogger.Error("failed resolving rootfs", "reason", rootfsResolveErr)
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

	cniMetadata := &configs.RunningVMMCNIMetadata{
		Config:        cniConfig,
		VethIfaceName: vethIfaceName,
		NetName:       machineConfig.MachineCNINetworkName,
		NetNS:         jailingFcConfig.NetNS,
	}

	cniMetadataBytes, jsonErr := json.Marshal(cniMetadata)
	if jsonErr != nil {
		rootLogger.Error("failed serializing CNI metadata to JSON", "reason", jsonErr)
		cleanup.CallAll() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

	if err := ioutil.WriteFile(filepath.Join(cacheDirectory, "cni"), []byte(cniMetadataBytes), 0644); err != nil {
		rootLogger.Error("failed writing CNI metadata the cache file", "reason", err)
		cleanup.CallAll() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

	// don't use resolvedRootfs below this point:
	machineConfig.
		WithDaemonize(commandConfig.Daemonize).
		WithKernelOverride(resolveKernel.HostPath()).
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
		Hostname:    commandConfig.Hostname, // TODO: validate that the hostname is a valid hostname string
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

	ifaceStaticConfig := startedMachine.NetworkInterfaces()[0].StaticConfiguration

	machinePid, pidErr := startedMachine.PID()
	if pidErr == nil {
		if err := pid.WritePIDToFile(cacheDirectory, machinePid); err != nil {
			rootLogger.Warn("pid write error", "reason", err)
		}
	} else {
		rootLogger.Warn("failed fetching machine PID", "reason", pidErr)
	}

	vmmLogger = vmmLogger.With("ip-address", ifaceStaticConfig.IPConfiguration.IPAddr.IP.String())

	if commandConfig.Daemonize {
		vmmLogger.Info("VMM running as a daemon",
			"ip-net", ifaceStaticConfig.IPConfiguration.IPAddr.String(),
			"jailer-dir", jailingFcConfig.JailerChrootDirectory(),
			"cache-dir", cacheDirectory,
			"pid", machinePid)
		os.Exit(0) // don't trigger defers
	}

	vmmLogger.Info("VMM running",
		"ip-net", ifaceStaticConfig.IPConfiguration.IPAddr.String(),
		"jailer-dir", jailingFcConfig.JailerChrootDirectory(),
		"cache-dir", cacheDirectory,
		"pid", machinePid)

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
