package run

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/combust-labs/firebuild/build/commands"
	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/naming"
	"github.com/combust-labs/firebuild/pkg/strategy"
	"github.com/combust-labs/firebuild/pkg/strategy/arbitrary"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/hashicorp/go-hclog"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

/*
	sudo /usr/local/go/bin/go run ./main.go run \
		--binary-firecracker=$(readlink /usr/bin/firecracker) \
		--binary-jailer=$(readlink /usr/bin/jailer) \
		--chroot-base=/srv/jailer \
		--from=tests/postgres:13 \
		--machine-cni-network-name=alpine \
		--machine-rootfs-base=/firecracker/rootfs \
		--machine-ssh-user=debian \
		--machine-vmlinux=/firecracker/vmlinux/vmlinux-v5.8 \
		--hostname=run-command-test \
		--env='VAR1=value1' \
		--env='VAR2=value2' \
		--env='VAR3=value3' \
		--log-as-json
*/

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
)

func initFlags() {
	Command.Flags().AddFlagSet(cniConfig.FlagSet())
	Command.Flags().AddFlagSet(commandConfig.FlagSet())
	Command.Flags().AddFlagSet(egressTestConfig.FlagSet())
	Command.Flags().AddFlagSet(jailingFcConfig.FlagSet())
	Command.Flags().AddFlagSet(logConfig.FlagSet())
	Command.Flags().AddFlagSet(machineConfig.FlagSet())
}

func init() {
	initFlags()
}

func run(cobraCommand *cobra.Command, _ []string) {

	cleanup := utils.NewDefers()
	defer cleanup.CallAll()

	rootLogger := logConfig.NewLogger("run")

	if err := machineConfig.Validate(); err != nil {
		rootLogger.Error("Configuration is invalid", "reason", err)
		os.Exit(1)
	}

	validatingConfigs := []configs.ValidatingConfig{
		commandConfig,
		jailingFcConfig,
		machineConfig,
	}

	for _, validatingConfig := range validatingConfigs {
		if err := validatingConfig.Validate(); err != nil {
			rootLogger.Error("configuration is invalid", "reason", err)
			os.Exit(1)
		}
	}

	vmmEnvironment, envErr := commandConfig.MergedEnvironment()
	if envErr != nil {
		rootLogger.Error("failed merging environment", "reason", envErr)
		os.Exit(1)
	}

	from := commands.From{BaseImage: commandConfig.From}
	structuredFrom := from.ToStructuredFrom()

	fileSystemSource := filepath.Join(machineConfig.MachineRootFSBase, "_builds",
		structuredFrom.Org(), structuredFrom.OS(), structuredFrom.Version(), naming.RootfsFileName)

	vethIfaceName := naming.GetRandomVethName()

	vmmLogger := rootLogger.With("vmm-id", jailingFcConfig.VMMID(), "veth-name", vethIfaceName)

	vmmLogger.Info("running VMM",
		"from", commandConfig.From,
		"source-rootfs", fileSystemSource,
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
			os.Exit(1)
		}
		strategyPublicKeys = append(strategyPublicKeys, sshPublicKey)
	}

	strategyConfig := &strategy.PseudoCloudInitHandlerConfig{
		Chroot:         jailingFcConfig.JailerChrootDirectory(),
		RootfsFileName: filepath.Base(fileSystemSource),
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
		WithRootFsHostPath(fileSystemSource).
		WithVethIfaceName(vethIfaceName)

	vmmCtx, vmmCancel := context.WithCancel(context.Background())
	cleanup.Add(func() {
		vmmCancel()
	})

	startedMachine, runErr := vmmProvider.Start(vmmCtx)
	if runErr != nil {
		vmmLogger.Error("firecracker VMM did not start, build failed", "reason", runErr)
		return
	}

	ifaceStaticConfig := startedMachine.NetworkInterfaces()[0].StaticConfiguration

	vmmLogger = vmmLogger.With("ip-address", ifaceStaticConfig.IPConfiguration.IPAddr.IP.String())

	vmmLogger.Info("VMM running", "ip-net", ifaceStaticConfig.IPConfiguration.IPAddr.String())

	chanStopStatus := installSignalHandlers(context.Background(), vmmLogger, startedMachine)

	startedMachine.Wait(context.Background())

	vmmLogger.Info("machine is stopped", "gracefully", <-chanStopStatus)

}

func installSignalHandlers(ctx context.Context, logger hclog.Logger, m vmm.StartedMachine) <-chan bool {
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
