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

type ProfileCommandConfig struct {
	flagBase
	ValidatingConfig `json:"-"`

	Profile        string
	ProfileConfDir string
}

func NewProfileCommandConfig() *ProfileCommandConfig {
	return &ProfileCommandConfig{}
}

func (c *ProfileCommandConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringVar(&c.Profile, "profile", "", "Configuration profile to apply")
		c.flagSet.StringVar(&c.ProfileConfDir, "profile-conf-dir", defaultProfileConfDir, "Profile configuration directory")
	}
	return c.flagSet
}

func (c *ProfileCommandConfig) Validate() error {
	if c.Profile == "" {
		return fmt.Errorf("--profile is empty")
	}
	return nil
}

type ProfileCreateConfig struct {
	flagBase
	ValidatingConfig `json:"-"`
	profilesModel.Profile

	Overwrite bool `json:"-"`
}

func NewProfileCreateConfig() *ProfileCreateConfig {
	return &ProfileCreateConfig{}
}

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

func (c *ProfileCreateConfig) GetMergedStorageConfig() map[string]interface{} {
	result := map[string]interface{}{}
	for k, v := range c.StorageProviderConfigStrings {
		result[k] = v
	}
	for k, v := range c.StorageProviderConfigInt64s {
		result[k] = v
	}
	return result
}

func (c *ProfileCreateConfig) Validate() error {
	// these can't be empty:
	if c.BinaryFirecracker == "" {
		return fmt.Errorf("--binary-firecracker is empty")
	}
	if c.BinaryJailer == "" {
		return fmt.Errorf("--binary-jailer is empty")
	}
	if c.ChrootBase == "" {
		return fmt.Errorf("--chroot-base is empty")
	}
	if c.RunCache == "" {
		return fmt.Errorf("--run-cache is empty")
	}

	// these must point to an existing location:
	if _, err := utils.CheckIfExistsAndIsRegular(c.BinaryFirecracker); err != nil {
		return errors.Wrap(err, "--binary-firecracker points to a non-existing location or not a regular file")
	}
	if _, err := utils.CheckIfExistsAndIsRegular(c.BinaryJailer); err != nil {
		return errors.Wrap(err, "--binary-jailer points to a non-existing location or not a regular file")
	}
	if _, err := utils.CheckIfExistsAndIsDirectory(c.ChrootBase); err != nil {
		return errors.Wrap(err, "--chroot-base points to a non-existing location or not a directory")
	}
	if _, err := utils.CheckIfExistsAndIsDirectory(c.RunCache); err != nil {
		return errors.Wrap(err, "--run-cache points to a non-existing location or not a directory")
	}

	if c.StorageProvider != "" {
		if p, err := resolver.GetStorageImplWithProvider(hclog.Default(), c.StorageProvider); p == nil || err != nil {
			return errors.Wrap(err, "configured --storage-provider could not be resolved")
		}
	}

	return nil
}
