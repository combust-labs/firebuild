package configs

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"golang.org/x/crypto/ssh"

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

	BootstrapCertsKeySize                int
	BootstrapCertsValidity               time.Duration
	BootstrapInitialCommunicationTimeout time.Duration
	BootstrapServerBindInterface         string

	// Dockerfile build:
	BuildArgs       map[string]string
	Dockerfile      string
	DockerfileStage string

	// Docker image build:
	DockerImage     string
	DockerImageBase string

	// Shared settings:
	PostBuildCommands []string
	PreBuildCommands  []string
	Tag               string
}

// NewRootfsCommandConfig returns new command configuration.
func NewRootfsCommandConfig() *RootfsCommandConfig {
	return &RootfsCommandConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *RootfsCommandConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.IntVar(&c.BootstrapCertsKeySize, "bootstrap-certs-key-size", 2048, "Embedded CA bootstrap certificates key size, recommended values: 2048 or 4096")
		c.flagSet.DurationVar(&c.BootstrapCertsValidity, "bootstrap-certs-validity", time.Minute*5, "The period for which the embedded bootstrap certificates are valid for")
		c.flagSet.DurationVar(&c.BootstrapInitialCommunicationTimeout, "bootstrap-initial-communication-timeout", time.Second*30, "Howlong to wait for vminit to initiate bootstrap with commands request before considering bootstrap failed")
		c.flagSet.StringVar(&c.BootstrapServerBindInterface, "bootstrap-server-bind-interface", "", "The interface to bind the bootstrap server on; if empty, a list of up broadcast up will be resolved and the first interface will be used")
		// Dockerfile build:
		c.flagSet.StringToStringVar(&c.BuildArgs, "build-arg", map[string]string{}, "Build arguments, Multiple OK")
		c.flagSet.StringVar(&c.Dockerfile, "dockerfile", "", "Local or remote (HTTP / HTTP) path; if the Dockerfile uses ADD or COPY commands, it's recommended to use a local file")
		c.flagSet.StringVar(&c.DockerfileStage, "dockerfile-stage", "", "The Dockerfile stage name to build from")
		// Docker image build:
		c.flagSet.StringVar(&c.DockerImage, "docker-image", "", "Docker image tag name to build from; mutually exclusive with --dockerfile")
		c.flagSet.StringVar(&c.DockerImageBase, "docker-image-base", "", "Rootfs base when building from Docker image, required because the base operating system can't be established from a Docker image; for example alpine:3.13")
		// Shared settings:
		c.flagSet.StringArrayVar(&c.PostBuildCommands, "post-build-command", []string{}, "OS specific commands to run after Dockerfile commands but before the file system is persisted, multiple OK")
		c.flagSet.StringArrayVar(&c.PreBuildCommands, "pre-build-command", []string{}, "OS specific commands to run before any Dockerfile command, multiple OK")
		c.flagSet.StringVar(&c.Tag, "tag", "", "Tag name of the build, required; must be org/name:version")
	}
	return c.flagSet
}

// Validate validates the correctness of the configuration.
func (c *RootfsCommandConfig) Validate() error {
	if c.Dockerfile != "" && c.DockerImage != "" {
		return fmt.Errorf("--dockerfile and --docker-image are mutually exclusive")
	}
	if c.DockerImage != "" {
		if c.DockerImageBase == "" {
			return fmt.Errorf("--docker-image-base is required when using --docker-image")
		}
	}
	return nil
}

// RunCommandConfig is the run command configuration.
type RunCommandConfig struct {
	flagBase
	ValidatingConfig

	Daemonize     bool
	EnvFiles      []string
	EnvVars       map[string]string
	From          string
	IdentityFiles []string
	Hostname      string
	Name          string
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
		c.flagSet.StringArrayVar(&c.IdentityFiles, "identity-file", []string{}, "Full path to the SSH public key to deploy to the machine during bootstrap, must be regular file, multiple OK")
		c.flagSet.StringVar(&c.Hostname, "hostname", "", "Hostname to apply to the VMM during bootstrap; if empty, a random name will be assigned")
		c.flagSet.StringVar(&c.Name, "name", "", "Name of the VM, maximum 20 characters; allowed characters: letters, digits, . and -")
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

// PublicKeys returns an array of ssh.PublicKey obtainer from identity files.
func (c *RunCommandConfig) PublicKeys() ([]ssh.PublicKey, error) {
	keys := []ssh.PublicKey{}
	for _, identityFile := range c.IdentityFiles {
		sshPublicKey, readErr := utils.SSHPublicKeyFromFile(identityFile)
		if readErr != nil {
			return keys, readErr
		}
		keys = append(keys, sshPublicKey)
	}
	return keys, nil
}

// Validate validates the correctness of the configuration.
func (c *RunCommandConfig) Validate() error {
	nameRegex := regexp.MustCompile("^[a-zA-Z0-9]{1,20}$")
	if c.Name != "" {
		if !nameRegex.MatchString(c.Name) {
			return fmt.Errorf("--name is not a valid name")
		}
	}
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
