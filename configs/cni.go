package configs

import (
	"github.com/spf13/pflag"
)

// CNIConfig provides CNI configuration options.
type CNIConfig struct {
	flagBase

	BinDir   string `json:"bin-dir"`
	ConfDir  string `json:"conf-dir"`
	CacheDir string `json:"cache-dir"`
}

// NewCNIConfig returns a new instance of the configuration.
func NewCNIConfig() *CNIConfig {
	return &CNIConfig{}
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *CNIConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringVar(&c.BinDir, "cni-bin-dir", "/opt/cni/bin", "CNI plugins binaries directory")
		c.flagSet.StringVar(&c.ConfDir, "cni-conf-dir", "/etc/cni/conf.d", "CNI configuration directory")
		c.flagSet.StringVar(&c.CacheDir, "cni-cache-dir", "/var/lib/cni", "CNI cache directory")
	}
	return c.flagSet
}

type RunningVMMCNIMetadata struct {
	Config        *CNIConfig `json:"config"`
	VethIfaceName string     `json:"veth-iface-name"`
	NetName       string     `json:"net-name"`
	NetNS         string     `json:"net-ns"`
}
