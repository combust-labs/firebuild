package configs

import (
	"github.com/spf13/pflag"
)

// MachineConfig provides machine configuration options.
type MachineConfig struct {
	flagBase

	CNINetworkName    string `json:"CniNetworkName" mapstructure:"CniNetworkName"`
	CPU               int64  `json:"CPU" mapstructure:"CPU"`
	CPUTemplate       string `json:"CPUTemplate" mapstructure:"CPUTemplate"`
	HTEnabled         bool   `json:"HTEnabled" mapstructure:"HTEnabled"`
	KernelArgs        string `json:"KernelArgs" mapstructure:"KernelArgs"`
	Mem               int64  `json:"Mem" mapstructure:"Mem"`
	NoMMDS            bool   `json:"NoMMDS" mapstructure:"NoMMDS"` // TODO: remove
	RootDrivePartUUID string `json:"RootDrivePartuuid" mapstructure:"RootDrivePartuuid"`
	SSHUser           string `json:"SSHUser" mapstructure:"SSHUser"`
	VMLinuxID         string `json:"VMLinux" mapstructure:"VMLinux"`

	LogFcHTTPCalls                 bool `json:"LogFirecrackerHTTPCalls" mapstructure:"LogFirecrackerHTTPCalls"`
	ShutdownGracefulTimeoutSeconds int  `json:"ShutdownGracefulTimeoutSeconds" mapstructure:"ShutdownGracefulTimeoutSeconds"`

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
		c.flagSet.StringVar(&c.CNINetworkName, "cni-network-name", "", "CNI network within which the build should run; it's recommended to use a dedicated network for build process")
		c.flagSet.Int64Var(&c.CPU, "cpu", 1, "Number of CPUs for the build VMM")
		c.flagSet.StringVar(&c.CPUTemplate, "cpu-template", "", "CPU template (empty, C2 or T3)")
		c.flagSet.BoolVar(&c.HTEnabled, "ht-enabled", false, "When specified, enable hyper-threading")
		c.flagSet.StringVar(&c.KernelArgs, "kernel-args", "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw", "Kernel arguments")
		c.flagSet.Int64Var(&c.Mem, "mem", 128, "Amount of memory for the VMM")
		c.flagSet.BoolVar(&c.NoMMDS, "no-mmds", false, "If set, disables MMDS")
		c.flagSet.StringVar(&c.RootDrivePartUUID, "root-drive-partuuid", "", "Root drive part UUID")
		c.flagSet.StringVar(&c.SSHUser, "ssh-user", "", "SSH user")
		c.flagSet.StringVar(&c.VMLinuxID, "vmlinux-id", "", "Kernel ID / name")

		c.flagSet.BoolVar(&c.LogFcHTTPCalls, "log-firecracker-http-calls", false, "If set, logs Firecracker HTTP client calls in debug mode")
		c.flagSet.IntVar(&c.ShutdownGracefulTimeoutSeconds, "shutdown-graceful-timeout-seconds", 30, "Graceful shutdown timeout before vmm is stopped forcefully")
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
