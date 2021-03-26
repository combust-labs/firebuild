package configs

import (
	"fmt"

	profilesModel "github.com/combust-labs/firebuild/pkg/profiles/model"
	"github.com/combust-labs/firebuild/pkg/storage/resolver"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/hashicorp/go-hclog"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
)

const defaultProfileConfDir = "/etc/firebuild/profiles"

// ProfileCommandConfig provides the command profile selection configuration options.
type ProfileCommandConfig struct {
	flagBase
	ValidatingConfig `json:"-"`

	Profile        string
	ProfileConfDir string
}

// NewProfileCommandConfig returns an initialized configuration instance.
func NewProfileCommandConfig() *ProfileCommandConfig {
	return &ProfileCommandConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *ProfileCommandConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringVar(&c.Profile, "profile", "", "Configuration profile to apply")
		c.flagSet.StringVar(&c.ProfileConfDir, "profile-conf-dir", defaultProfileConfDir, "Profile configuration directory")
	}
	return c.flagSet
}

// Validate validates the correctness of the configuration.
func (c *ProfileCommandConfig) Validate() error {
	if c.Profile == "" {
		return fmt.Errorf("--profile is empty")
	}
	return nil
}

// ProfileCreateConfig represents the profile create command configuration.
type ProfileCreateConfig struct {
	flagBase
	ValidatingConfig `json:"-"`
	profilesModel.Profile

	Overwrite bool `json:"-"`
}

// NewProfileCreateConfig returns an initialized configuration instance.
func NewProfileCreateConfig() *ProfileCreateConfig {
	return &ProfileCreateConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *ProfileCreateConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringVar(&c.BinaryFirecracker, "binary-firecracker", "", "Path to the Firecracker binary to use")
		c.flagSet.StringVar(&c.BinaryJailer, "binary-jailer", "", "Path to the Firecracker Jailer binary to use")
		c.flagSet.StringVar(&c.ChrootBase, "chroot-base", "", "chroot base directory; can't be empty or /")
		c.flagSet.StringVar(&c.RunCache, "run-cache", "", "Firebuild run cache directory")
		c.flagSet.StringVar(&c.StorageProvider, "storage-provider", "", "Storage provider to use for the profile")
		c.flagSet.StringToStringVar(&c.StorageProviderConfigStrings, "storage-provider-property-string", map[string]string{}, "Storage provider configuration string property, multiple OK")
		c.flagSet.StringToInt64Var(&c.StorageProviderConfigInt64s, "storage-provider-property-int64", map[string]int64{}, "Storage provider configuration int64 property, multiple OK")
		c.flagSet.BoolVar(&c.TracingEnable, "tracing-enable", false, "Enable tracing")
		c.flagSet.StringVar(&c.TracingCollectorHostPort, "tracing-collector-host-port", "", "Host port of the tracing collector")
		c.flagSet.BoolVar(&c.TracingLogEnable, "tracing-log-enable", false, "If set, enables tracer logging")
		c.flagSet.BoolVar(&c.Overwrite, "overwrite", false, "If profile already exists, overwrite")
	}
	return c.flagSet
}

// Validate validates the correctness of the configuration.
func (c *ProfileCreateConfig) Validate() error {

	// these must point to an existing location:
	if c.BinaryFirecracker != "" {
		if _, err := utils.CheckIfExistsAndIsRegular(c.BinaryFirecracker); err != nil {
			return errors.Wrap(err, "--binary-firecracker points to a non-existing location or not a regular file")
		}
	}
	if c.BinaryJailer != "" {
		if _, err := utils.CheckIfExistsAndIsRegular(c.BinaryJailer); err != nil {
			return errors.Wrap(err, "--binary-jailer points to a non-existing location or not a regular file")
		}
	}
	if c.ChrootBase != "" {
		if len(c.ChrootBase) > ChrootBaseMaxLength {
			return fmt.Errorf("--chroot-base must cannot be longer than %d characters", ChrootBaseMaxLength)
		}
		if _, err := utils.CheckIfExistsAndIsDirectory(c.ChrootBase); err != nil {
			return errors.Wrap(err, "--chroot-base points to a non-existing location or not a directory")
		}
	}
	if c.RunCache == "" {
		if _, err := utils.CheckIfExistsAndIsDirectory(c.RunCache); err != nil {
			return errors.Wrap(err, "--run-cache points to a non-existing location or not a directory")
		}
	}

	if c.StorageProvider != "" {
		if p, err := resolver.NewDefaultResolver().GetStorageImplWithProvider(hclog.Default(), c.StorageProvider); p == nil || err != nil {
			return errors.Wrap(err, "configured --storage-provider could not be resolved")
		}
	}

	return nil
}
