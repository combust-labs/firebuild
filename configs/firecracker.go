package configs

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/gofrs/uuid"
	"github.com/spf13/pflag"
)

// JailingFirecrackerConfig represents firecracker specific configuration options.
type JailingFirecrackerConfig struct {
	sync.Mutex
	flagBase

	BinaryFirecracker string
	BinaryJailer      string
	ChrootBase        string

	JailerGID      int
	JailerNumeNode int
	JailerUID      int

	NetNS string

	vmmID string
}

// NewJailingFirecrackerConfig returns a new instance of the configuration.
func NewJailingFirecrackerConfig() *JailingFirecrackerConfig {
	cfg := &JailingFirecrackerConfig{}
	return cfg.ensure()
}

// JailerChrootDirectory returns a full path to the jailer configuration directory.
// This method will return empty string until the flag set returned by FlagSet() is parsed.
func (c *JailingFirecrackerConfig) JailerChrootDirectory() string {
	return filepath.Join(c.ChrootBase,
		filepath.Base(c.BinaryFirecracker), c.VMMID())
}

// VMMID returns a configuration instance unique VMM ID.
func (c *JailingFirecrackerConfig) VMMID() string {
	return c.vmmID
}

// FlagSet returns an instance of the flag set for the configuration.
func (c *JailingFirecrackerConfig) FlagSet() *pflag.FlagSet {
	if c.initFlagSet() {
		c.flagSet.StringVar(&c.BinaryFirecracker, "binary-firecracker", "", "Path to the Firecracker binary to use")
		c.flagSet.StringVar(&c.BinaryJailer, "binary-jailer", "", "Path to the Firecracker Jailer binary to use")
		c.flagSet.StringVar(&c.ChrootBase, "chroot-base", "/srv/jailer", "chroot base directory")
		c.flagSet.IntVar(&c.JailerGID, "jailer-gid", 0, "Jailer GID value")
		c.flagSet.IntVar(&c.JailerNumeNode, "jailer-numa-node", 0, "Jailer NUMA node")
		c.flagSet.IntVar(&c.JailerUID, "jailer-uid", 0, "Jailer UID value")
		c.flagSet.StringVar(&c.NetNS, "netns", "/var/lib/netns", "Network namespace")
	}
	return c.flagSet
}

func (c *JailingFirecrackerConfig) ensure() *JailingFirecrackerConfig {
	if c.vmmID == "" {
		c.vmmID = strings.ReplaceAll(uuid.Must(uuid.NewV4()).String(), "-", "")
	}
	return c
}
