package configs

import (
	"github.com/hashicorp/go-hclog"
	"github.com/spf13/pflag"
)

// LogConfig represents logging configuration.
type LogConfig struct {
	flagBase

	LogLevel      string
	LogColor      bool
	LogForceColor bool
	LogAsJSON     bool
}

// NewLogginConfig returns a new logging configuration.
func NewLogginConfig() *LogConfig {
	return &LogConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *LogConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringVar(&c.LogLevel, "log-level", "debug", "Log level")
		c.flagSet.BoolVar(&c.LogAsJSON, "log-as-json", false, "Log as JSON")
		c.flagSet.BoolVar(&c.LogColor, "log-color", false, "Log in color")
		c.flagSet.BoolVar(&c.LogForceColor, "log-force-color", false, "Force colored log output")
	}
	return c.flagSet
}

// NewLogger returns a new configured logger.
func (c *LogConfig) NewLogger(name string) hclog.Logger {
	loggerColorOption := hclog.ColorOff
	if c.LogColor {
		loggerColorOption = hclog.AutoColor
	}
	if c.LogForceColor {
		loggerColorOption = hclog.ForceColor
	}

	return hclog.New(&hclog.LoggerOptions{
		Name:       name,
		Level:      hclog.LevelFromString(c.LogLevel),
		Color:      loggerColorOption,
		JSONFormat: c.LogAsJSON,
	})
}
