package configs

import (
	profileModel "github.com/combust-labs/firebuild/pkg/profiles/model"
	"github.com/spf13/pflag"
)

// TracingConfig is the tracing configuration.
type TracingConfig struct {
	flagBase
	ProfileInheriting `json:"-"`

	ApplicationName string
	Enable          bool
	HostPort        string
	LogEnable       bool
}

// NewTracingConfig returns a new instance of the configuration.
func NewTracingConfig(appName string) *TracingConfig {
	return &TracingConfig{
		ApplicationName: appName,
	}
}

// UpdateFromProfile updates the configuration from a profile.
func (c *TracingConfig) UpdateFromProfile(input *profileModel.Profile) error {
	if input.TracingEnable {
		c.Enable = input.TracingEnable
	}
	if input.TracingLogEnable {
		c.LogEnable = input.TracingLogEnable
	}
	if input.TracingCollectorHostPort != "" {
		c.HostPort = input.TracingCollectorHostPort
	}
	return nil
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *TracingConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.BoolVar(&c.Enable, "tracing-enable", false, "If set, enables tracing")
		c.flagSet.StringVar(&c.HostPort, "tracing-collector-host-port", "127.0.0.1:6831", "Host port of the collector")
		c.flagSet.BoolVar(&c.LogEnable, "tracing-log-enable", false, "If set, enables tracer logging")
	}
	return c.flagSet
}
