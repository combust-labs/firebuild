package configs

import "github.com/spf13/pflag"

// TracingConfig is the tracing configuration.
type TracingConfig struct {
	flagBase

	ApplicationName string
	Enable          bool
	HostPort        string
	LogEnable       bool
}

// NewCNIConfig returns a new instance of the configuration.
func NewTracingConfig(appName string) *TracingConfig {
	return &TracingConfig{
		ApplicationName: appName,
	}
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
