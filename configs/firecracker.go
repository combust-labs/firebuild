package configs

import (
	"io"
	"os"
	"path/filepath"

	"github.com/combust-labs/firebuild/pkg/strategy/arbitrary"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
)

// DefaultVethIfaceName is the default veth interface name.
const DefaultVethIfaceName = "veth0"

// DefaultFirectackerStrategy returns an instance of the default Firecracker Jailer strategy for a given machine config.
func DefaultFirectackerStrategy(machineConfig *MachineConfig) arbitrary.PlacingStrategy {
	return arbitrary.NewStrategy(func() *arbitrary.HandlerPlacement {
		return arbitrary.NewHandlerPlacement(firecracker.
			LinkFilesHandler(filepath.Base(machineConfig.KernelOverride())),
			firecracker.CreateLogFilesHandlerName)
	})
}

// FcConfigProvider is a Firecracker SDK configuration builder provider.
type FcConfigProvider interface {
	ToSDKConfig() firecracker.Config
	WithHandlersAdapter(firecracker.HandlersAdapter) FcConfigProvider
	WithVethIfaceName(string) FcConfigProvider
}

type defaultFcConfigProvider struct {
	jailingFcConfig *JailingFirecrackerConfig
	machineConfig   *MachineConfig

	fcStrategy    firecracker.HandlersAdapter
	vethIfaceName string
}

// NewFcConfigProvider creates a new builder provider.
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
		KernelImagePath: c.machineConfig.KernelOverride(),
		KernelArgs:      c.machineConfig.KernelArgs,
		NetNS:           c.jailingFcConfig.NetNS,
		Drives: []models.Drive{
			{
				DriveID:      firecracker.String("1"),
				PathOnHost:   firecracker.String(c.machineConfig.RootfsOverride()),
				IsRootDevice: firecracker.Bool(true),
				IsReadOnly:   firecracker.Bool(false),
				Partuuid:     c.machineConfig.RootDrivePartUUID,
			},
		},
		NetworkInterfaces: []firecracker.NetworkInterface{{
			AllowMMDS: !c.machineConfig.NoMMDS,
			CNIConfiguration: &firecracker.CNIConfiguration{
				NetworkName: c.machineConfig.CNINetworkName,
				IfName:      c.vethIfaceName,
			},
		}},
		VsockDevices: []firecracker.VsockDevice{},
		MachineCfg: models.MachineConfiguration{
			VcpuCount:   firecracker.Int64(c.machineConfig.CPU),
			CPUTemplate: models.CPUTemplate(c.machineConfig.CPUTemplate),
			HtEnabled:   firecracker.Bool(c.machineConfig.HTEnabled),
			MemSizeMib:  firecracker.Int64(c.machineConfig.Mem),
		},
		JailerCfg: &firecracker.JailerConfig{
			GID:           firecracker.Int(c.jailingFcConfig.JailerGID),
			UID:           firecracker.Int(c.jailingFcConfig.JailerUID),
			ID:            c.jailingFcConfig.VMMID(),
			NumaNode:      firecracker.Int(c.jailingFcConfig.JailerNumeNode),
			ExecFile:      c.jailingFcConfig.BinaryFirecracker,
			JailerBinary:  c.jailingFcConfig.BinaryJailer,
			ChrootBaseDir: c.jailingFcConfig.ChrootBase,
			Daemonize:     c.machineConfig.Daemonize(),
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

func (c *defaultFcConfigProvider) WithVethIfaceName(input string) FcConfigProvider {
	c.vethIfaceName = input
	return c
}
