package configs

import (
	"github.com/spf13/pflag"
)

// MachineConfig provides options for the machine agress test.
type MachineConfig struct {
	flagBase

	MachineCNINetworkName        string
	MachineCPUTemplate           string
	MachineHTEnabled             bool
	MachineKernelArgs            string
	MachineRootDrivePartUUID     string
	MachineSSHEnableAgentForward bool
	MachineSSHPort               int
	MachineSSHUser               string
	MachineSSHAuthorizedKeysFile string
	MachineVMLinuxID             string
	ResourcesCPU                 int64
	ResourcesMem                 int64

	ShutdownGracefulTimeoutSeconds int

	daemonize      bool
	kernelOverride string
	rootfsOverride string
}

// NewMachineConfig returns a new instance of the configuration.
func NewMachineConfig() *MachineConfig {
	return &MachineConfig{
		kernelOverride: "call-with-kernel-override",
		rootfsOverride: "call-with-rootfs-override",
	}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *MachineConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringVar(&c.MachineCNINetworkName, "machine-cni-network-name", "", "CNI network within which the build should run. It's recommended to use a dedicated network for build process")
		c.flagSet.StringVar(&c.MachineCPUTemplate, "machine-cpu-template", "", "CPU template (empty, C2 or T3)")
		c.flagSet.BoolVar(&c.MachineHTEnabled, "machine-ht-enabled", false, "When specified, enable hyper-threading")
		c.flagSet.StringVar(&c.MachineKernelArgs, "machine-kernel-args", "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw", "Kernel arguments")
		c.flagSet.StringVar(&c.MachineRootDrivePartUUID, "machine-root-drive-partuuid", "", "Root drive part UUID")
		c.flagSet.BoolVar(&c.MachineSSHEnableAgentForward, "machine-ssh-enable-agent-forward", false, "If set, enables SSH agent forward")
		c.flagSet.IntVar(&c.MachineSSHPort, "machine-ssh-port", 22, "SSH port")
		c.flagSet.StringVar(&c.MachineSSHUser, "machine-ssh-user", "", "SSH user")
		c.flagSet.StringVar(&c.MachineSSHAuthorizedKeysFile, "machine-ssh-authorized-keys-file", "", "SSH authorized keys file in the machine root file system; if empty, /home/{ssh-user}/.ssh/authorized_keys is asumed")
		c.flagSet.StringVar(&c.MachineVMLinuxID, "machine-vmlinux-id", "", "Kernel ID / name")
		c.flagSet.Int64Var(&c.ResourcesCPU, "resources-cpu", 1, "Number of CPU for the build VMM")
		c.flagSet.Int64Var(&c.ResourcesMem, "resources-mem", 128, "Amount of memory for the VMM")
		c.flagSet.IntVar(&c.ShutdownGracefulTimeoutSeconds, "shutdown-graceful-timeout-seconds", 30, "Grafeul shotdown timeout before vmm is stopped forcefully")
	}
	return c.flagSet
}

func (c *MachineConfig) Daemonize() bool {
	return c.daemonize
}

func (c *MachineConfig) KernelOverride() string {
	return c.kernelOverride
}

func (c *MachineConfig) RootfsOverride() string {
	return c.rootfsOverride
}

func (c *MachineConfig) WithDaemonize(input bool) *MachineConfig {
	c.daemonize = input
	return c
}
func (c *MachineConfig) WithKernelOverride(input string) *MachineConfig {
	c.kernelOverride = input
	return c
}

func (c *MachineConfig) WithRootfsOverride(input string) *MachineConfig {
	c.rootfsOverride = input
	return c
}
