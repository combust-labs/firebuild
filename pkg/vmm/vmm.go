package vmm

import (
	"context"
	"fmt"

	"github.com/combust-labs/firebuild/configs"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/hashicorp/go-hclog"
	"github.com/sirupsen/logrus"
)

// StoppedOK is the VMM stopped status.
type StoppedOK = bool

var (
	// StoppedGracefully indicates the machine was stopped gracefully.
	StoppedGracefully = StoppedOK(true)
	// StoppedForcefully indicates that the machine did not stop gracefully
	// and the shutdown had to be forced.
	StoppedForcefully = StoppedOK(false)
)

// Provider abstracts the configuration required to start a VMM.
type Provider interface {
	// Start starts the VMM.
	Start(context.Context) (StartedMachine, error)

	WithHandlersAdapter(firecracker.HandlersAdapter) Provider
	WithRootFsHostPath(string) Provider
	WithVethIfaceName(string) Provider
}

type defaultProvider struct {
	cniConfig       *configs.CNIConfig
	jailingFcConfig *configs.JailingFirecrackerConfig
	machineConfig   *configs.MachineConfig

	handlersAdapter firecracker.HandlersAdapter
	logger          hclog.Logger
	machine         *firecracker.Machine
	rootFsHostPath  string
	vethIfaceName   string
}

// NewDefaultProvider creates a default provider.
func NewDefaultProvider(cniConfig *configs.CNIConfig, jailingFcConfig *configs.JailingFirecrackerConfig, machineConfig *configs.MachineConfig) Provider {
	return &defaultProvider{
		cniConfig:       cniConfig,
		jailingFcConfig: jailingFcConfig,
		machineConfig:   machineConfig,

		handlersAdapter: configs.DefaultFirectackerStrategy(machineConfig),
		logger:          hclog.Default(),
		rootFsHostPath:  machineConfig.MachineRootFSBase,
		vethIfaceName:   configs.DefaultVethIfaceName,
	}
}

func (p *defaultProvider) Start(ctx context.Context) (StartedMachine, error) {
	vmmLogger := logrus.New()
	machineOpts := []firecracker.Opt{
		firecracker.WithLogger(logrus.NewEntry(vmmLogger)),
	}
	fcConfig := configs.NewFcConfigProvider(p.jailingFcConfig, p.machineConfig).
		WithHandlersAdapter(p.handlersAdapter).
		WithVethIfaceName(p.vethIfaceName).
		WithRootFsHostPath(p.rootFsHostPath).
		ToSDKConfig()
	m, err := firecracker.NewMachine(ctx, fcConfig, machineOpts...)
	if err != nil {
		return nil, fmt.Errorf("Failed creating machine: %s", err)
	}
	if err := m.Start(ctx); err != nil {
		return nil, fmt.Errorf("Failed to start machine: %v", err)
	}

	return &defaultStartedMachine{
		cniConfig:     p.cniConfig,
		machineConfig: p.machineConfig,
		logger:        p.logger,
		machine:       m,
		vethIfaceName: p.vethIfaceName,
	}, nil
}

func (p *defaultProvider) WithHandlersAdapter(input firecracker.HandlersAdapter) Provider {
	p.handlersAdapter = input
	return p
}

func (p *defaultProvider) WithRootFsHostPath(input string) Provider {
	p.rootFsHostPath = input
	return p
}

func (p *defaultProvider) WithVethIfaceName(input string) Provider {
	p.vethIfaceName = input
	return p
}
