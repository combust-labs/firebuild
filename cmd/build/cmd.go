package build

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/appministry/firebuild/buildcontext"
	"github.com/appministry/firebuild/buildcontext/commands"
	"github.com/appministry/firebuild/buildcontext/utils"
	"github.com/appministry/firebuild/configs"
	"github.com/appministry/firebuild/remote"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/gofrs/uuid"
	"github.com/hashicorp/go-hclog"
	"github.com/spf13/cobra"

	"github.com/sirupsen/logrus"

	"github.com/containernetworking/cni/libcni"
)

type stoppedOK = bool

// Command is the build command declaration.
var Command = &cobra.Command{
	Use:   "build",
	Short: "Build A VMM root file system from a Docker image",
	Run:   run,
	Long:  ``,
}

type buildConfig struct {
	BinaryFirecracker string
	BinaryJailer      string
	ChrootBase        string
	Dockerfile        string

	InitCommands []string

	JailerGID      int
	JailerNumeNode int
	JailerUID      int

	MachineCNINetworkName         string
	MachineCPUTemplate            string
	MachineKernelArgs             string
	MachineRootFSBase             string
	MachineRootDrivePartUUID      string
	MachineSSHKey                 string
	MachineSSHDisableAgentForward bool
	MachineSSHPort                int
	MachineSSHUser                string
	MachineVMLinux                string
	NetNS                         string

	ResourcesCPU int64
	ResourcesMem int64

	ShutdownGracefulTimeoutSeconds int
}

type cniConfig struct {
	BinDir   string
	ConfDir  string
	CacheDir string
}

var (
	commandConfig        = new(buildConfig)
	commandCniConfig     = new(cniConfig)
	logConfig            = new(configs.LogConfig)
	rootFSCopyBufferSize = 4 * 1024 * 1024
	stoppedGracefully    = stoppedOK(true)
	stoppedForcefully    = stoppedOK(false)
)

func initFlags() {
	Command.Flags().StringVar(&commandConfig.BinaryFirecracker, "binary-firecracker", "", "Path to the Firecracker binary to use")
	Command.Flags().StringVar(&commandConfig.BinaryJailer, "binary-jailer", "", "Path to the Firecracker Jailer binary to use")
	Command.Flags().StringVar(&commandConfig.ChrootBase, "chroot-base", "/srv/jailer", "chroot base directory")
	Command.Flags().StringVar(&commandConfig.Dockerfile, "dockerfile", "", "Local or remote (HTTP / HTTP) path; if the Dockerfile uses ADD or COPY commands, it's recommended to use a local file")

	Command.Flags().StringArrayVar(&commandConfig.InitCommands, "init-command", []string{}, "OS specific init commands to run before any Dockerfile command.")

	Command.Flags().IntVar(&commandConfig.JailerGID, "jailer-gid", 0, "Jailer GID value")
	Command.Flags().IntVar(&commandConfig.JailerNumeNode, "jailer-numa-node", 0, "Jailer NUMA node")
	Command.Flags().IntVar(&commandConfig.JailerUID, "jailer-uid", 0, "Jailer UID value")

	Command.Flags().StringVar(&commandConfig.MachineCNINetworkName, "machine-cni-network-name", "", "CNI network within which the build should run. It's recommended to use a dedicated network for build process")
	Command.Flags().StringVar(&commandConfig.MachineCPUTemplate, "machine-cpu-template", "", "CPU template (empty, C2 or T3)")
	Command.Flags().StringVar(&commandConfig.MachineKernelArgs, "machine-kernel-args", "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw", "Kernel arguments")
	Command.Flags().StringVar(&commandConfig.MachineRootFSBase, "machine-rootfs-base", "", "Root directory where operating system file systems reside")
	Command.Flags().StringVar(&commandConfig.MachineRootDrivePartUUID, "machine-root-drive-partuuid", "", "Root drive part UUID")
	Command.Flags().StringVar(&commandConfig.MachineSSHKey, "machine-ssh-key-file", "", "Path to the SSH key file")
	Command.Flags().BoolVar(&commandConfig.MachineSSHDisableAgentForward, "machine-ssh-disable-agent-forward", false, "If set, disables SSH agent forward")
	Command.Flags().IntVar(&commandConfig.MachineSSHPort, "machine-ssh-port", 22, "SSH port")
	Command.Flags().StringVar(&commandConfig.MachineSSHUser, "machine-ssh-user", "", "SSH user")
	Command.Flags().StringVar(&commandConfig.MachineVMLinux, "machine-vmlinux", "", "Kernel file path")
	Command.Flags().StringVar(&commandConfig.NetNS, "netns", "/var/lib/netns", "Network namespace")

	Command.Flags().IntVar(&commandConfig.ShutdownGracefulTimeoutSeconds, "shutdown-graceful-timeout-seconds", 30, "Grafeul shotdown timeout before vmm is stopped forcefully")

	Command.Flags().StringVar(&commandCniConfig.BinDir, "cni-bin-dir", "/opt/cni/bin", "CNI plugins binaries directory")
	Command.Flags().StringVar(&commandCniConfig.ConfDir, "cni-conf-dir", "/etc/cni/conf.d", "CNI configuration directory")
	Command.Flags().StringVar(&commandCniConfig.CacheDir, "cni-cache-dir", "/var/lib/cni", "CNI cache directory")

	Command.Flags().StringVar(&logConfig.LogLevel, "log-level", "debug", "Log level")
	Command.Flags().BoolVar(&logConfig.LogAsJSON, "log-as-json", false, "Log as JSON")
	Command.Flags().BoolVar(&logConfig.LogColor, "log-color", false, "Log in color")
	Command.Flags().BoolVar(&logConfig.LogForceColor, "log-force-color", false, "Force colored log output")

	Command.Flags().Int64Var(&commandConfig.ResourcesCPU, "resources-cpu", 1, "Number of CPU for the build VMM")
	Command.Flags().Int64Var(&commandConfig.ResourcesMem, "resources-mem", 128, "Amount of memory for the VMM")
}

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
	initFlags()
}

func run(cobraCommand *cobra.Command, _ []string) {

	cleanup := &defers{fs: []func(){}}
	defer cleanup.exec()

	rootLogger := configs.NewLogger("build", logConfig)

	tempDirectory, err := ioutil.TempDir("", "")
	if err != nil {
		rootLogger.Error("Failed creating temporary build directory", "reason", err)
		os.Exit(1)
	}

	// The first thing to do is to resolve the Dockerfile:
	buildContext, err := buildcontext.NewFromString(commandConfig.Dockerfile, tempDirectory)
	if err != nil {
		rootLogger.Error("failed parsing Dockerfile", "reason", err)
		os.Exit(1)
	}

	base := buildContext.From()
	if base == nil {
		rootLogger.Error("no base to build from")
		cleanup.exec() // manually - defers don't run on os.Exit
		os.Exit(1)
	}
	structuredBase := base.ToStructuredFrom()

	cleanup.add(func() {
		rootLogger.Info("cleaning up temp build directory")
		status := os.RemoveAll(tempDirectory)
		rootLogger.Info("temp build directory removal status", "error", status)
	})

	// TODO: check that it exists and is regular file
	sourceRootfs := filepath.Join(commandConfig.MachineRootFSBase, structuredBase.Org(), structuredBase.OS(), structuredBase.Version(), "root.ext4")
	buildRootfs := filepath.Join(tempDirectory, "rootfs")

	vmmID := strings.ReplaceAll(uuid.Must(uuid.NewV4()).String(), "-", "")

	jailDirectory := filepath.Join(commandConfig.ChrootBase,
		filepath.Base(commandConfig.BinaryFirecracker), vmmID)

	if err := copyFile(sourceRootfs, buildRootfs, rootFSCopyBufferSize); err != nil {
		rootLogger.Error("failed copying requested rootfs to temp build location",
			"source", sourceRootfs,
			"target", buildRootfs,
			"reason", err)
		cleanup.exec() // manually - defers don't run on os.Exit
		os.Exit(1)
	}

	vethIfaceName := getRandomVethName()

	vmmLogger := rootLogger.With("vmm-id", vmmID, "veth-name", vethIfaceName)

	vmmLogger.Info("buildiing VMM",
		"dockerfile", commandConfig.Dockerfile,
		"source-rootfs", buildRootfs,
		"origin-rootfs", sourceRootfs,
		"jail", jailDirectory)

	cleanup.add(func() {
		vmmLogger.Info("cleaning up jail directory")
		status := os.RemoveAll(jailDirectory)
		vmmLogger.Info("jail directory removal status", "error", status)
	})

	var fifo io.WriteCloser // TODO: do it like firectl does it

	fcConfig := firecracker.Config{
		SocketPath:      "",      // given via Jailer
		LogFifo:         "",      // CONSIDER: make this configurable
		LogLevel:        "debug", // CONSIDER: make this configurable
		MetricsFifo:     "",      // not configurable for the build machines
		FifoLogWriter:   fifo,
		KernelImagePath: commandConfig.MachineVMLinux,
		KernelArgs:      commandConfig.MachineKernelArgs,
		NetNS:           commandConfig.NetNS,
		Drives: []models.Drive{
			{
				DriveID:      firecracker.String("1"),
				PathOnHost:   firecracker.String(buildRootfs),
				IsRootDevice: firecracker.Bool(true),
				IsReadOnly:   firecracker.Bool(false),
				Partuuid:     commandConfig.MachineRootDrivePartUUID,
			},
		},
		NetworkInterfaces: []firecracker.NetworkInterface{{
			CNIConfiguration: &firecracker.CNIConfiguration{
				NetworkName: commandConfig.MachineCNINetworkName,
				IfName:      vethIfaceName,
			},
		}},
		VsockDevices: []firecracker.VsockDevice{},
		MachineCfg: models.MachineConfiguration{
			VcpuCount:   firecracker.Int64(commandConfig.ResourcesCPU),
			CPUTemplate: models.CPUTemplate(commandConfig.MachineCPUTemplate),
			HtEnabled:   firecracker.Bool(false),
			MemSizeMib:  firecracker.Int64(commandConfig.ResourcesMem),
		},
		JailerCfg: &firecracker.JailerConfig{
			GID:            firecracker.Int(commandConfig.JailerGID),
			UID:            firecracker.Int(commandConfig.JailerUID),
			ID:             vmmID,
			NumaNode:       firecracker.Int(commandConfig.JailerNumeNode),
			ExecFile:       commandConfig.BinaryFirecracker,
			JailerBinary:   commandConfig.BinaryJailer,
			ChrootBaseDir:  commandConfig.ChrootBase,
			Daemonize:      false,
			ChrootStrategy: firecracker.NewNaiveChrootStrategy(commandConfig.MachineVMLinux),
			Stdout:         os.Stdout,
			Stderr:         os.Stderr,
			// do not pass stdin because the build VMM does not require input
			// and it messes up the terminal
			Stdin: nil,
		},
		VMID: vmmID,
	}

	vmmCtx, vmmCancel := context.WithCancel(context.Background())
	cleanup.add(func() {
		vmmCancel()
	})

	machine, runErr := runVMM(vmmCtx, fcConfig)
	if runErr != nil {
		vmmLogger.Error("Firecracker VMM did not start, build failed", "reason", runErr)
		return
	}

	ifaceStaticConfig := fcConfig.NetworkInterfaces[0].StaticConfiguration

	vmmLogger = vmmLogger.With("ip-address", ifaceStaticConfig.IPConfiguration.IPAddr.IP.String())

	vmmLogger.Info("VMM running", "ip-net", ifaceStaticConfig.IPConfiguration.IPAddr.String())

	remoteClient, remoteErr := remote.Connect(context.Background(), remote.ConnectConfig{
		SSKKeyFile:          commandConfig.MachineSSHKey,
		SSHUsername:         commandConfig.MachineSSHUser,
		IP:                  ifaceStaticConfig.IPConfiguration.IPAddr.IP,
		Port:                commandConfig.MachineSSHPort,
		DisableAgentForward: commandConfig.MachineSSHDisableAgentForward,
	}, vmmLogger.Named("remote-client"))

	if remoteErr != nil {
		stopVMM(vmmCtx, machine, vethIfaceName, nil, vmmLogger)
		vmmLogger.Error("Failed connecting to remote", "reason", remoteErr)
		return
	}

	vmmLogger.Info("Connected via SSH")

	// TODO: replace with VMM based egress testing
	time.Sleep(time.Second * 10)

	initCommands := []commands.Run{}
	for _, initCmd := range commandConfig.InitCommands {
		initCommands = append(initCommands, commands.RunWithDefaults(initCmd))
	}

	if buildErr := buildContext.WithLogger(vmmLogger.Named("builder")).Build(remoteClient, initCommands...); err != nil {
		stopVMM(vmmCtx, machine, vethIfaceName, remoteClient, vmmLogger)
		vmmLogger.Error("Failed boostrapping remote via SSH", "reason", buildErr)
		return
	}

	go func() {
		if stopVMM(vmmCtx, machine, vethIfaceName, remoteClient, vmmLogger) == stoppedForcefully {
			vmmLogger.Warn("Machine was not stopped gracefully, see previous errors. It's possible that the file system may not be complete. Retry or proceed with caution.")
		}
	}()

	vmmLogger.Info("Waiting for machine to stop...")

	machine.Wait(context.Background())

	vmmLogger.Info("Machine to stopped. Persisting the file system...")

	// TODO: handle moving the boostrapped file system here

}

func stopVMM(ctx context.Context, machine *firecracker.Machine, vethIfaceName string, remoteClient remote.ConnectedClient, logger hclog.Logger) stoppedOK {

	if remoteClient != nil {
		logger.Info("Closing remote client...")
		status := remoteClient.Close()
		logger.Info("Remote client closed", "error", status)
	}

	shutdownCtx, cancelFunc := context.WithTimeout(ctx, time.Second*time.Duration(commandConfig.ShutdownGracefulTimeoutSeconds))
	defer cancelFunc()

	logger.Info("Attempting VMM graceful shutdown...")

	chanStopped := make(chan error, 1)
	go func() {
		// Ask the machine to shut down so the file system gets flushed
		// and out changes are written to disk.
		chanStopped <- machine.Shutdown(shutdownCtx)
	}()

	stoppedState := stoppedForcefully

	select {
	case stopErr := <-chanStopped:
		if stopErr != nil {
			logger.Warn("VMM stopped with error but within timeout", "reason", stopErr)
			logger.Warn("VMM stopped forcefully", "error", machine.StopVMM())
		} else {
			logger.Warn("VMM stopped gracefully")
			stoppedState = stoppedGracefully
		}
	case <-shutdownCtx.Done():
		logger.Warn("VMM failed to stop gracefully: timeout reached")
		logger.Warn("VMM stopped forcefully", "error", machine.StopVMM())
	}

	logger.Info("Cleaning up CNI network...")

	cniCleanupErr := cleanupCNINetwork(commandCniConfig,
		commandConfig.NetNS,
		machine.Cfg.VMID,
		commandConfig.MachineCNINetworkName,
		vethIfaceName)

	logger.Info("CNI network cleanup status", "error", cniCleanupErr)

	return stoppedState
}

func runVMM(ctx context.Context, fcConfig firecracker.Config) (*firecracker.Machine, error) {
	vmmLogger := logrus.New()
	machineOpts := []firecracker.Opt{
		firecracker.WithLogger(logrus.NewEntry(vmmLogger)),
	}
	m, err := firecracker.NewMachine(ctx, fcConfig, machineOpts...)
	if err != nil {
		return nil, fmt.Errorf("Failed creating machine: %s", err)
	}
	if err := m.Start(ctx); err != nil {
		return nil, fmt.Errorf("Failed to start machine: %v", err)
	}
	return m, nil
}

func copyFile(source, dest string, bufferSize int) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return nil
	}
	destFile, err := os.Create(dest)
	if err != nil {
		return nil
	}
	buf := make([]byte, bufferSize)
	for {
		n, err := sourceFile.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		if _, err := destFile.Write(buf[:n]); err != nil {
			return err
		}
	}
	return nil
}

func cleanupCNINetwork(cniConfig *cniConfig, netNs, vmmID, networkName, ifname string) error {
	cniPlugin := libcni.NewCNIConfigWithCacheDir([]string{cniConfig.BinDir}, cniConfig.CacheDir, nil)
	networkConfig, err := libcni.LoadConfList(cniConfig.ConfDir, networkName)
	if err != nil {
		return err
	}
	if err := cniPlugin.DelNetworkList(context.Background(), networkConfig, &libcni.RuntimeConf{
		ContainerID: vmmID, // golang firecracker SDK uses the VMID, if VMID is set
		NetNS:       netNs,
		IfName:      ifname,
	}); err != nil {
		return err
	}
	return nil
}

func getRandomVethName() string {
	return fmt.Sprintf("veth%s", utils.RandStringBytes(11))
}
