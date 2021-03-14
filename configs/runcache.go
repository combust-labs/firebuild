package configs

import (
	"fmt"

	profileModel "github.com/combust-labs/firebuild/pkg/profiles/model"
	"github.com/spf13/pflag"
)

// RunCacheConfig contains the run cache settings.
type RunCacheConfig struct {
	flagBase
	ProfileInheriting
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

// UpdateFromProfile updates the configuration from a profile.
func (c *RunCacheConfig) UpdateFromProfile(input *profileModel.Profile) error {
	if input.RunCache != "" {
		c.RunCache = input.RunCache
	}
	return nil
}

// Validate validates the correctness of the configuration.
func (c *RunCacheConfig) Validate() error {
	if c.RunCache == "" || c.RunCache == "/" {
		return fmt.Errorf("run cache cannot be empty or /")
	}
	return nil
}
