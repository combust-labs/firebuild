package configs

import (
	"github.com/spf13/pflag"
)

// MachineConfig provides options for the machine agress test.
type MachineConfig struct {
	flagBase

	CNINetworkName        string `json:"cni-network-name" mapstructure:"cni-network-name"`
	CPU                   int64  `json:"cpu" mapstructure:"cpu"`
	CPUTemplate           string `json:"cpu-template" mapstructure:"cpu-template"`
	HTEnabled             bool   `json:"ht-enabled" mapstructure:"ht-enabled"`
	KernelArgs            string `json:"kernel-args" mapstructure:"kernel-args"`
	Mem                   int64  `json:"mem" mapstructure:"mem"`
	MMDS                  bool   `json:"mmds" mapstructure:"mmds"`
	RootDrivePartUUID     string `json:"root-drive-partuuid" mapstructure:"root-drive-partuuid"`
	SSHEnableAgentForward bool   `json:"ssh-enable-agent-forward" mapstructure:"ssh-enable-agent-forward"`
	SSHPort               int    `json:"ssh-port" mapstructure:"ssh-port"`
	SSHUser               string `json:"ssh-user" mapstructure:"ssh-user"`
	SSHAuthorizedKeysFile string `json:"ssh-authorized-keys-file" mapstructure:"ssh-authorized-keys-file"`
	VMLinuxID             string `json:"vmlinux" mapstructure:"vmlinux"`

	LogFcHTTPCalls                 bool `json:"log-firecracker-http-calls" mapstructure:"log-firecracker-http-calls"`
	ShutdownGracefulTimeoutSeconds int  `json:"shutdown-graceful-timeout-seconds" mapstructure:"shutdown-graceful-timeout-seconds"`

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
		c.flagSet.StringVar(&c.CNINetworkName, "cni-network-name", "", "CNI network within which the build should run. It's recommended to use a dedicated network for build process")
		c.flagSet.Int64Var(&c.CPU, "cpu", 1, "Number of CPU for the build VMM")
		c.flagSet.StringVar(&c.CPUTemplate, "cpu-template", "", "CPU template (empty, C2 or T3)")
		c.flagSet.BoolVar(&c.HTEnabled, "ht-enabled", false, "When specified, enable hyper-threading")
		c.flagSet.StringVar(&c.KernelArgs, "kernel-args", "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw", "Kernel arguments")
		c.flagSet.Int64Var(&c.Mem, "mem", 128, "Amount of memory for the VMM")
		c.flagSet.BoolVar(&c.MMDS, "mmds", false, "If set, enables MMDS")
		c.flagSet.StringVar(&c.RootDrivePartUUID, "root-drive-partuuid", "", "Root drive part UUID")
		c.flagSet.BoolVar(&c.SSHEnableAgentForward, "ssh-enable-agent-forward", false, "If set, enables SSH agent forward")
		c.flagSet.StringVar(&c.SSHAuthorizedKeysFile, "ssh-authorized-keys-file", "", "SSH authorized keys file in the machine root file system; if empty, /home/{ssh-user}/.ssh/authorized_keys is asumed")
		c.flagSet.IntVar(&c.SSHPort, "ssh-port", 22, "SSH port")
		c.flagSet.StringVar(&c.SSHUser, "ssh-user", "", "SSH user")
		c.flagSet.StringVar(&c.VMLinuxID, "vmlinux-id", "", "Kernel ID / name")

		c.flagSet.BoolVar(&c.LogFcHTTPCalls, "log-firecracker-http-calls", false, "If set, logs Firecracker HTTP client calls in debug mode")
		c.flagSet.IntVar(&c.ShutdownGracefulTimeoutSeconds, "shutdown-graceful-timeout-seconds", 30, "Grafeul shotdown timeout before vmm is stopped forcefully")
	}
	return c.flagSet
}

// Daemonize returns the configured daemonize setting.
func (c *MachineConfig) Daemonize() bool {
	return c.daemonize
}

// KernelOverride returns the configured kernel setting.
func (c *MachineConfig) KernelOverride() string {
	return c.kernelOverride
}

// RootfsOverride returns the configured rootfs setting.
func (c *MachineConfig) RootfsOverride() string {
	return c.rootfsOverride
}

// WithDaemonize sets the daemonize setting.
func (c *MachineConfig) WithDaemonize(input bool) *MachineConfig {
	c.daemonize = input
	return c
}

// WithKernelOverride sets the ketting setting.
func (c *MachineConfig) WithKernelOverride(input string) *MachineConfig {
	c.kernelOverride = input
	return c
}

// WithRootfsOverride sets the rootfs setting.
func (c *MachineConfig) WithRootfsOverride(input string) *MachineConfig {
	c.rootfsOverride = input
	return c
}
