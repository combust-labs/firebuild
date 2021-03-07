package configs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/combust-labs/firebuild/pkg/strategy/arbitrary"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/gofrs/uuid"
	"github.com/spf13/pflag"
)

// DefaultVethIfaceName is the default veth interface name.
const DefaultVethIfaceName = "veth0"

// DefaultFirectackerStrategy returns an instance of the default Firecracker Jailer strategy for a given machine config.
func DefaultFirectackerStrategy(machineConfig *MachineConfig) arbitrary.Strategy {
	return arbitrary.NewStrategy(func() *arbitrary.HandlerWithRequirement {
		return arbitrary.NewHandlerWithRequirement(firecracker.
			LinkFilesHandler(filepath.Base(machineConfig.MachineVMLinux)),
			firecracker.CreateLogFilesHandlerName)
	})
}

// JailingFirecrackerConfig represents firecracker specific configuration options.
type JailingFirecrackerConfig struct {
	sync.Mutex
	flagBase
	ValidatingConfig

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
		c.flagSet.StringVar(&c.ChrootBase, "chroot-base", "/srv/jailer", "chroot base directory; can't be empty or /")
		c.flagSet.IntVar(&c.JailerGID, "jailer-gid", 0, "Jailer GID value")
		c.flagSet.IntVar(&c.JailerNumeNode, "jailer-numa-node", 0, "Jailer NUMA node")
		c.flagSet.IntVar(&c.JailerUID, "jailer-uid", 0, "Jailer UID value")
		c.flagSet.StringVar(&c.NetNS, "netns", "/var/lib/netns", "Network namespace")
	}
	return c.flagSet
}

// Validate validates the correctness of the configuration.
func (c *JailingFirecrackerConfig) Validate() error {
	if c.ChrootBase == "" || c.ChrootBase == "/" {
		return fmt.Errorf("--chroot-base must be set to value other than empty and /")
	}
	return nil
}

func (c *JailingFirecrackerConfig) ensure() *JailingFirecrackerConfig {
	if c.vmmID == "" {
		c.vmmID = strings.ReplaceAll(uuid.Must(uuid.NewV4()).String(), "-", "")
	}
	return c
}

type FcConfigProvider interface {
	ToSDKConfig() firecracker.Config
	WithHandlersAdapter(firecracker.HandlersAdapter) FcConfigProvider
	WithRootFsHostPath(string) FcConfigProvider
	WithVethIfaceName(string) FcConfigProvider
}

type defaultFcConfigProvider struct {
	jailingFcConfig *JailingFirecrackerConfig
	machineConfig   *MachineConfig

	fcStrategy     firecracker.HandlersAdapter
	rootFsHostPath string
	vethIfaceName  string
}

func NewFcConfigProvider(jailingFcConfig *JailingFirecrackerConfig, machineConfig *MachineConfig) FcConfigProvider {
	return &defaultFcConfigProvider{
		jailingFcConfig: jailingFcConfig,
		machineConfig:   machineConfig,
		vethIfaceName:   DefaultVethIfaceName,
	}
}

func (c *defaultFcConfigProvider) ToSDKConfig() firecracker.Config {

	var fifo io.WriteCloser // TODO: do it like firectl does it

	return firecracker.Config{
		SocketPath:      "",      // given via Jailer
		LogFifo:         "",      // CONSIDER: make this configurable
		LogLevel:        "debug", // CONSIDER: make this configurable
		MetricsFifo:     "",      // not configurable for the build machines
		FifoLogWriter:   fifo,
		KernelImagePath: c.machineConfig.MachineVMLinux,
		KernelArgs:      c.machineConfig.MachineKernelArgs,
		NetNS:           c.jailingFcConfig.NetNS,
		Drives: []models.Drive{
			{
				DriveID:      firecracker.String("1"),
				PathOnHost:   firecracker.String(c.rootFsHostPath),
				IsRootDevice: firecracker.Bool(true),
				IsReadOnly:   firecracker.Bool(false),
				Partuuid:     c.machineConfig.MachineRootDrivePartUUID,
			},
		},
		NetworkInterfaces: []firecracker.NetworkInterface{{
			CNIConfiguration: &firecracker.CNIConfiguration{
				NetworkName: c.machineConfig.MachineCNINetworkName,
				IfName:      c.vethIfaceName,
			},
		}},
		VsockDevices: []firecracker.VsockDevice{},
		MachineCfg: models.MachineConfiguration{
			VcpuCount:   firecracker.Int64(c.machineConfig.ResourcesCPU),
			CPUTemplate: models.CPUTemplate(c.machineConfig.MachineCPUTemplate),
			HtEnabled:   firecracker.Bool(false),
			MemSizeMib:  firecracker.Int64(c.machineConfig.ResourcesMem),
		},
		JailerCfg: &firecracker.JailerConfig{
			GID:           firecracker.Int(c.jailingFcConfig.JailerGID),
			UID:           firecracker.Int(c.jailingFcConfig.JailerUID),
			ID:            c.jailingFcConfig.VMMID(),
			NumaNode:      firecracker.Int(c.jailingFcConfig.JailerNumeNode),
			ExecFile:      c.jailingFcConfig.BinaryFirecracker,
			JailerBinary:  c.jailingFcConfig.BinaryJailer,
			ChrootBaseDir: c.jailingFcConfig.ChrootBase,
			Daemonize:     false,
			ChrootStrategy: func() firecracker.HandlersAdapter {
				if c.fcStrategy == nil {
					return DefaultFirectackerStrategy(c.machineConfig)
				}
				return c.fcStrategy
			}(),
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			// do not pass stdin because the build VMM does not require input
			// and it messes up the terminal
			Stdin: nil,
		},
		VMID: c.jailingFcConfig.VMMID(),
	}
}

func (c *defaultFcConfigProvider) WithHandlersAdapter(input firecracker.HandlersAdapter) FcConfigProvider {
	c.fcStrategy = input
	return c
}

func (c *defaultFcConfigProvider) WithRootFsHostPath(input string) FcConfigProvider {
	c.rootFsHostPath = input
	return c
}

func (c *defaultFcConfigProvider) WithVethIfaceName(input string) FcConfigProvider {
	c.vethIfaceName = input
	return c
}
