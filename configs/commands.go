package configs

import (
	"fmt"
	"os"
	"time"

	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"github.com/subosito/gotenv"
)

// BaseOSCommandConfig is the baseos command configuration.
type BaseOSCommandConfig struct {
	flagBase

	Dockerfile string
	FSSizeMBs  int
	Tag        string
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
		c.flagSet.StringVar(&c.Tag, "tag", "", "Tag name; if empty, the name FROM value from the Dockerfile will be used")
	}
	return c.flagSet
}

// KillCommandConfig is the kill command configuration.
type KillCommandConfig struct {
	flagBase
	ValidatingConfig

	ShutdownTimeout time.Duration
	VMMID           string
}

// NewKillCommandConfig returns new command configuration.
func NewKillCommandConfig() *KillCommandConfig {
	return &KillCommandConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *KillCommandConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.DurationVar(&c.ShutdownTimeout, "shutdown-timeout", time.Second*15, "If the VMM is running and shutdown is called, how long to wait for clean shutdown")
		c.flagSet.StringVar(&c.VMMID, "vmm-id", "", "ID of the VMM to kill")
	}
	return c.flagSet
}

// Validate validates the correctness of the configuration.
func (c *KillCommandConfig) Validate() error {
	if c.VMMID == "" {
		return fmt.Errorf("--vmm-id can't be empty")
	}
	return nil
}

// InspectCommandConfig is the inspect command configuration.
type InspectCommandConfig struct {
	flagBase
	ValidatingConfig

	VMMID string
}

// NewInspectCommandConfig returns new command configuration.
func NewInspectCommandConfig() *InspectCommandConfig {
	return &InspectCommandConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *InspectCommandConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringVar(&c.VMMID, "vmm-id", "", "ID of the VMM to inspect")
	}
	return c.flagSet
}

// Validate validates the correctness of the configuration.
func (c *InspectCommandConfig) Validate() error {
	if c.VMMID == "" {
		return fmt.Errorf("--vmm-id can't be empty")
	}
	return nil
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

// RunCacheConfig contains the run cache settings.
type RunCacheConfig struct {
	flagBase
	ValidatingConfig

	RunCache string
}

// NewRunCacheConfig returns new run cahce command configuration.
func NewRunCacheConfig() *RunCacheConfig {
	return &RunCacheConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *RunCacheConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringVar(&c.RunCache, "run-cache", "/var/lib/firebuild", "Firebuild run cache directory")
	}
	return c.flagSet
}

// Validate validates the correctness of the configuration.
func (c *RunCacheConfig) Validate() error {
	if c.RunCache == "" || c.RunCache == "/" {
		return fmt.Errorf("run cache cannot be empty or /")
	}
	return nil
}

// RunCommandConfig is the run command configuration.
type RunCommandConfig struct {
	flagBase
	ValidatingConfig

	Daemonize    bool
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
		c.flagSet.BoolVar(&c.Daemonize, "daemonize", false, "When set, runs the VMM in the detached mode")
		c.flagSet.StringArrayVar(&c.EnvFiles, "env-file", []string{}, "Full path to an environment file to apply to the VMM during bootstrap, multiple OK")
		c.flagSet.StringToStringVar(&c.EnvVars, "env", map[string]string{}, "Additional environment variables to apply to the VMM during bootstrap, multiple OK")
		c.flagSet.StringVar(&c.From, "from", "", "The image to launch from, for example: tests/postgres:13")
		c.flagSet.StringVar(&c.IdentityFile, "identity-file", "", "Full path to the SSH public key to deploy to the machine during bootstrap, must be regular file")
		c.flagSet.StringVar(&c.Hostname, "hostname", "", "Hostname to apply to the VMM during bootstrap; if empty, a random name will be assigned")
	}
	return c.flagSet
}

// MergedEnvironment returns merged envirionment declared by the configuration.
// The order of merging:
//  - parse each env file in order provided
//  - apply all individual --env values
// Duplicated values are always overriden.
func (c *RunCommandConfig) MergedEnvironment() (map[string]string, error) {
	env := map[string]string{}
	for _, envFile := range c.EnvFiles {
		f, openErr := os.Open(envFile)
		if openErr != nil {
			return env, errors.Wrapf(openErr, "failed opening environment file '%s' for reading", envFile)
		}
		defer f.Close()
		partialEnv, parseErr := gotenv.StrictParse(f)
		if parseErr != nil {
			return env, errors.Wrapf(parseErr, "failed parsing environment file '%s'", envFile)
		}
		for k, v := range partialEnv {
			env[k] = v
		}
	}
	for k, v := range c.EnvVars {
		env[k] = v
	}
	return env, nil
}

// Validate validates the correctness of the configuration.
func (c *RunCommandConfig) Validate() error {
	for _, envFile := range c.EnvFiles {
		if _, statErr := utils.CheckIfExistsAndIsRegular(envFile); statErr != nil {
			return errors.Wrapf(statErr, "environment file '%s' stat error", envFile)
		}
	}
	if !utils.IsValidHostname(c.Hostname) {
		return fmt.Errorf("string '%s' is not a valid hostname", c.Hostname)
	}
	return nil
}
