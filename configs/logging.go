package configs

import (
	"github.com/hashicorp/go-hclog"
)

// LogConfig represents logging configuration.
type LogConfig struct {
	LogLevel      string
	LogColor      bool
	LogForceColor bool
	LogAsJSON     bool
}

// NewLogger returns a new configured logger.
func NewLogger(name string, logConfig *LogConfig) hclog.Logger {
	loggerColorOption := hclog.ColorOff
	if logConfig.LogColor {
		loggerColorOption = hclog.AutoColor
	}
	if logConfig.LogForceColor {
		loggerColorOption = hclog.ForceColor
	}

	return hclog.New(&hclog.LoggerOptions{
		Name:       name,
		Level:      hclog.LevelFromString(logConfig.LogLevel),
		Color:      loggerColorOption,
		JSONFormat: logConfig.LogAsJSON,
	})
}
