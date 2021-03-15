package configs

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	profileModel "github.com/combust-labs/firebuild/pkg/profiles/model"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/spf13/pflag"
)

// JailingFirecrackerConfig represents Jailerspecific configuration options.
type JailingFirecrackerConfig struct {
	sync.Mutex
	flagBase
	ProfileInheriting `json:"-"`
	ValidatingConfig  `json:"-"`

	BinaryFirecracker string `json:"binary-firecracker" mapstructure:"binary-firecracker"`
	BinaryJailer      string `json:"binary-jailer" mapstructure:"binary-jailer"`
	ChrootBase        string `json:"chroot-base" mapstructure:"chroot-base"`

	JailerGID      int `json:"jailer-gid" mapstructure:"jailer-gid"`
	JailerNumeNode int `json:"jailer-numa-node" mapstructure:"jailer-numa-node"`
	JailerUID      int `json:"jailer-uid" mapstructure:"jailer-uid"`

	NetNS string `json:"netns" mapstructure:"netns"`

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
		c.flagSet.StringVar(&c.ChrootBase, "chroot-base", "/srv/jailer", "chroot base directory; can't be empty or /")
		c.flagSet.IntVar(&c.JailerGID, "jailer-gid", 0, "Jailer GID value")
		c.flagSet.IntVar(&c.JailerNumeNode, "jailer-numa-node", 0, "Jailer NUMA node")
		c.flagSet.IntVar(&c.JailerUID, "jailer-uid", 0, "Jailer UID value")
		c.flagSet.StringVar(&c.NetNS, "netns", "/var/lib/netns", "Network namespace")
	}
	return c.flagSet
}

// UpdateFromProfile updates the configuration from a profile.
func (c *JailingFirecrackerConfig) UpdateFromProfile(input *profileModel.Profile) error {
	if input.BinaryFirecracker != "" {
		c.BinaryFirecracker = input.BinaryFirecracker
	}
	if input.BinaryJailer != "" {
		c.BinaryJailer = input.BinaryJailer
	}
	if input.ChrootBase != "" {
		c.ChrootBase = input.ChrootBase
	}
	return nil
}

// Validate validates the correctness of the configuration.
func (c *JailingFirecrackerConfig) Validate() error {
	if c.ChrootBase == "" || c.ChrootBase == "/" {
		return fmt.Errorf("--chroot-base must be set to value other than empty and /")
	}
	return nil
}

// WithVMMID allows overriding the VMM ID.
func (c *JailingFirecrackerConfig) WithVMMID(input string) *JailingFirecrackerConfig {
	c.vmmID = input
	return c
}

func (c *JailingFirecrackerConfig) ensure() *JailingFirecrackerConfig {
	if c.vmmID == "" {
		c.vmmID = strings.ToLower(utils.RandStringWithDigitsBytes(20))
	}
	return c
}
