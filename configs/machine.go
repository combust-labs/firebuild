package configs

import "github.com/spf13/pflag"

// MachineConfig provides options for the machine agress test.
type MachineConfig struct {
	flagBase

	MachineCNINetworkName        string
	MachineCPUTemplate           string
	MachineKernelArgs            string
	MachineRootFSBase            string
	MachineRootDrivePartUUID     string
	MachineSSHEnableAgentForward bool
	MachineSSHPort               int
	MachineSSHUser               string
	MachineSSHAuthorizedKeysFile string
	MachineVMLinux               string
	ResourcesCPU                 int64
	ResourcesMem                 int64
}

// NewMachineConfig returns a new instance of the configuration.
func NewMachineConfig() *MachineConfig {
	return &MachineConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *MachineConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringVar(&c.MachineCNINetworkName, "machine-cni-network-name", "", "CNI network within which the build should run. It's recommended to use a dedicated network for build process")
		c.flagSet.StringVar(&c.MachineCPUTemplate, "machine-cpu-template", "", "CPU template (empty, C2 or T3)")
		c.flagSet.StringVar(&c.MachineKernelArgs, "machine-kernel-args", "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw", "Kernel arguments")
		c.flagSet.StringVar(&c.MachineRootFSBase, "machine-rootfs-base", "", "Root directory where operating system file systems reside, required, can't be /")
		c.flagSet.StringVar(&c.MachineRootDrivePartUUID, "machine-root-drive-partuuid", "", "Root drive part UUID")
		c.flagSet.BoolVar(&c.MachineSSHEnableAgentForward, "machine-ssh-enable-agent-forward", false, "If set, enables SSH agent forward")
		c.flagSet.IntVar(&c.MachineSSHPort, "machine-ssh-port", 22, "SSH port")
		c.flagSet.StringVar(&c.MachineSSHUser, "machine-ssh-user", "", "SSH user")
		c.flagSet.StringVar(&c.MachineSSHAuthorizedKeysFile, "machine-ssh-authorized-keys-file", "", "SSH authorized keys file in the machine root file system; if empty, /home/{ssh-user}/.ssh/authorized_keys is asumed")
		c.flagSet.StringVar(&c.MachineVMLinux, "machine-vmlinux", "", "Kernel file path")
		c.flagSet.Int64Var(&c.ResourcesCPU, "resources-cpu", 1, "Number of CPU for the build VMM")
		c.flagSet.Int64Var(&c.ResourcesMem, "resources-mem", 128, "Amount of memory for the VMM")
	}
	return c.flagSet
}
