package configs

import "github.com/spf13/pflag"

// BaseOSCommandConfig is the baseos command configuration.
type BaseOSCommandConfig struct {
	flagBase

	Dockerfile        string
	FSSizeMBs         int
	MachineRootFSBase string
}

// NewBaseOSCommandConfig returns new command configuration.
func NewBaseOSCommandConfig() *BaseOSCommandConfig {
	return &BaseOSCommandConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *BaseOSCommandConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringVar(&c.Dockerfile, "dockerfile", "", "Full path to the base OS Dockerfile")
		c.flagSet.IntVar(&c.FSSizeMBs, "filesystem-size-mbs", 500, "File system size in megabytes")
		c.flagSet.StringVar(&c.MachineRootFSBase, "machine-rootfs-base", "", "Root directory where operating system file systems reside, required, can't be /")
	}
	return c.flagSet
}

// RootfsCommandConfig is the rootfs command configuration.
type RootfsCommandConfig struct {
	flagBase

	BuildArgs            map[string]string
	Dockerfile           string
	PostBuildCommands    []string
	PreBuildCommands     []string
	ServiceFileInstaller string
	Tag                  string
}

// NewRootfsCommandConfig returns new command configuration.
func NewRootfsCommandConfig() *RootfsCommandConfig {
	return &RootfsCommandConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *RootfsCommandConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringToStringVar(&c.BuildArgs, "build-arg", map[string]string{}, "Build arguments, Multiple OK")
		c.flagSet.StringVar(&c.Dockerfile, "dockerfile", "", "Local or remote (HTTP / HTTP) path; if the Dockerfile uses ADD or COPY commands, it's recommended to use a local file")
		c.flagSet.StringArrayVar(&c.PostBuildCommands, "post-build-command", []string{}, "OS specific commands to run after Dockerfile commands but before the file system is persisted, multiple OK")
		c.flagSet.StringArrayVar(&c.PreBuildCommands, "pre-build-command", []string{}, "OS specific commands to run before any Dockerfile command, multiple OK")
		c.flagSet.StringVar(&c.ServiceFileInstaller, "service-file-installer", "", "Local path to the program to upload to the VMM and install the service file")
		c.flagSet.StringVar(&c.Tag, "tag", "", "Tag name of the build, required; must be org/name:version")
	}
	return c.flagSet
}

// RunCommandConfig is the run command configuration.
type RunCommandConfig struct {
	flagBase

	EnvFiles     []string
	EnvVars      map[string]string
	From         string
	IdentityFile string
	Hostname     string
}

// NewRunCommandConfig returns new command configuration.
func NewRunCommandConfig() *RunCommandConfig {
	return &RunCommandConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *RunCommandConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringArrayVar(&c.EnvFiles, "env-file", []string{}, "Full path to an environment file to apply to the VMM during bootstrap, multiple OK")
		c.flagSet.StringToStringVar(&c.EnvVars, "env", map[string]string{}, "Additional environment variables to apply to the VMM during bootstrap, multiple OK")
		c.flagSet.StringVar(&c.From, "from", "", "The image to launch from, for example: tests/postgres:13")
		c.flagSet.StringVar(&c.IdentityFile, "identity-file", "", "Full path to the SSH public key to deploy to the machine during bootstrap, must be regular file")
		c.flagSet.StringVar(&c.Hostname, "hostname", "vmm", "Hostname to apply to the VMM during bootstrap")
	}
	return c.flagSet
}
