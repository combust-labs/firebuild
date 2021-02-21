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
	"github.com/appministry/firebuild/remote"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/gofrs/uuid"
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
	rootFSCopyBufferSize = 4 * 1024 * 1024
	stoppedGracefully    = stoppedOK(true)
	stoppedForcefully    = stoppedOK(false)
)

func initFlags() {
	Command.Flags().StringVar(&commandConfig.BinaryFirecracker, "binary-firecracker", "", "Path to the Firecracker binary to use")
	Command.Flags().StringVar(&commandConfig.BinaryJailer, "binary-jailer", "", "Path to the Firecracker Jailer binary to use")
	Command.Flags().StringVar(&commandConfig.ChrootBase, "chroot-base", "/srv/jailer", "chroot base directory")
	Command.Flags().StringVar(&commandConfig.Dockerfile, "dockerfile", "", "Local or remote (HTTP / HTTP) path; if the Dockerfile uses ADD or COPY commands, it's recommended to use a local file")

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

	Command.Flags().Int64Var(&commandConfig.ResourcesCPU, "resources-cpu", 1, "Number of CPU for the build VMM")
	Command.Flags().Int64Var(&commandConfig.ResourcesMem, "resources-mem", 128, "Amount of memory for the VMM")
}

func init() {
	rand.Seed(time.Now().UTC().UnixNano())
	initFlags()
}

func run(cobraCommand *cobra.Command, _ []string) {

	// The first thing to do is to resolve the Dockerfile:
	buildContext, err := buildcontext.NewFromString(commandConfig.Dockerfile)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	base := buildContext.From()
	if base == nil {
		fmt.Println("no base to build from")
		os.Exit(1)
	}
	structuredBase := base.ToStructuredFrom()

	tempDirectory, err := ioutil.TempDir("", "")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer func() {
		fmt.Println("Cleaning up temp build directory:", os.RemoveAll(tempDirectory))
	}()

	// TODO: check that it exists and is regular file
	sourceRootfs := filepath.Join(commandConfig.MachineRootFSBase, structuredBase.Org(), structuredBase.OS(), structuredBase.Version(), "root.ext4")
	buildRootfs := filepath.Join(tempDirectory, "rootfs")

	vmmID := strings.ReplaceAll(uuid.Must(uuid.NewV4()).String(), "-", "")

	jailDirectory := filepath.Join(commandConfig.ChrootBase,
		filepath.Base(commandConfig.BinaryFirecracker), vmmID)

	if err := copyFile(sourceRootfs, buildRootfs, rootFSCopyBufferSize); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println("Building from", commandConfig.Dockerfile, "using rootfs", sourceRootfs, "copied to", buildRootfs)
	fmt.Println("Jail directory", jailDirectory, "will be cleaned up automatically")
	vethIfaceName := getRandomVethName()
	fmt.Println("Going to use", vethIfaceName, "as veth interface name")

	defer func() {
		fmt.Println("Cleaning up jail directory:", os.RemoveAll(jailDirectory))
	}()

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
	defer func() {
		fmt.Println("Calling cancel on the runVMM context")
		vmmCancel()
	}()
	machine, runErr := runVMM(vmmCtx, fcConfig)
	if runErr != nil {
		fmt.Println("Machine did not start, reason:", runErr)
	}

	ifaceStaticConfig := fcConfig.NetworkInterfaces[0].StaticConfiguration
	fmt.Println("Machine is started. IP net address is:",
		ifaceStaticConfig.IPConfiguration.IPAddr.String(),
		"IP address is:",
		ifaceStaticConfig.IPConfiguration.IPAddr.IP.String())

	remoteClient, remoteErr := remote.Connect(context.Background(), remote.ConnectConfig{
		SSKKeyFile:          commandConfig.MachineSSHKey,
		SSHUsername:         commandConfig.MachineSSHUser,
		IP:                  ifaceStaticConfig.IPConfiguration.IPAddr.IP,
		Port:                commandConfig.MachineSSHPort,
		DisableAgentForward: commandConfig.MachineSSHDisableAgentForward,
	})

	if remoteErr != nil {
		stopVMM(vmmCtx, machine, vethIfaceName, nil)
		fmt.Println("Could not connect to remote:", remoteErr)
		return
	}

	fmt.Println("Connected via SSH connect to remote:", remoteClient)

	time.Sleep(time.Second * 10)

	initCommands := []commands.Run{
		commands.RunWithDefaults("rm -rf /var/cache/apk && mkdir -p /var/cache/apk && sudo apk update"),
	}

	if err := buildContext.Build(remoteClient, initCommands...); err != nil {
		stopVMM(vmmCtx, machine, vethIfaceName, remoteClient)
		fmt.Println("Failed bootstrapping remote:", remoteErr)
		return
	}

	go func() {
		if stopVMM(vmmCtx, machine, vethIfaceName, remoteClient) == stoppedForcefully {
			fmt.Println("WARNING: Machine was not stopped gracefully, see previous errors. It's possible that the file system may not be complete. Retry or proceed with caution.")
		}
	}()

	machine.Wait(context.Background())

	fmt.Println("Persisting file system...")

	// TODO: handle moving the boostrapped file system here

}

func stopVMM(ctx context.Context, machine *firecracker.Machine, vethIfaceName string, remoteClient remote.ConnectedClient) stoppedOK {

	if remoteClient != nil {
		fmt.Println("Closing remote client...")
		fmt.Println("Remote client closed:", remoteClient.Close())
	}

	shutdownCtx, cancelFunc := context.WithTimeout(ctx, time.Second*time.Duration(commandConfig.ShutdownGracefulTimeoutSeconds))
	defer cancelFunc()
	fmt.Println("Stopping VMM gracefully...")
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
			fmt.Println("VMM stopped with error:", stopErr, "stopping forcefully:", machine.StopVMM())
		} else {
			fmt.Println("VMM stopped gracefully")
			stoppedState = stoppedGracefully
		}
	case <-shutdownCtx.Done():
		fmt.Println("VMM failed to stop gracefully within the timeout, stopping forcefully:", machine.StopVMM())
	}

	cniCleanupErr := cleanupCNINetwork(commandCniConfig,
		commandConfig.NetNS,
		machine.Cfg.VMID,
		commandConfig.MachineCNINetworkName,
		vethIfaceName)

	fmt.Println("Cleaning up CNI network interface:", cniCleanupErr)

	return stoppedState
}

func runVMM(ctx context.Context, fcConfig firecracker.Config) (*firecracker.Machine, error) {
	logger := logrus.New()
	machineOpts := []firecracker.Opt{
		firecracker.WithLogger(logrus.NewEntry(logger)),
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
