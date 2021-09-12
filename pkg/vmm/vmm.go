package vmm

import (
	"context"
	"fmt"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/vmm/chroot"
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
	WithVethIfaceName(string) Provider
}

type defaultProvider struct {
	cniConfig       *configs.CNIConfig
	jailingFcConfig *configs.JailingFirecrackerConfig
	machineConfig   *configs.MachineConfig

	handlersAdapter firecracker.HandlersAdapter
	logger          hclog.Logger
	machine         *firecracker.Machine
	vethIfaceName   string
}

// NewDefaultProvider creates a default provider.
func NewDefaultProvider(cniConfig *configs.CNIConfig, jailingFcConfig *configs.JailingFirecrackerConfig, machineConfig *configs.MachineConfig) Provider {
	return &defaultProvider{
		cniConfig:       cniConfig,
		jailingFcConfig: jailingFcConfig,
		machineConfig:   machineConfig,

		handlersAdapter: configs.DefaultFirecrackerStrategy(machineConfig),
		logger:          hclog.Default(),
		vethIfaceName:   configs.DefaultVethIfaceName,
	}
}

func (p *defaultProvider) Start(ctx context.Context) (StartedMachine, error) {

	machineChroot := chroot.NewWithLocation(chroot.LocationFromComponents(p.jailingFcConfig.JailerChrootDirectory(),
		p.jailingFcConfig.BinaryFirecracker,
		p.jailingFcConfig.VMMID()))

	vmmLoggerEntry := logrus.NewEntry(logrus.New())
	machineOpts := []firecracker.Opt{
		firecracker.WithLogger(vmmLoggerEntry),
	}

	if p.machineConfig.LogFcHTTPCalls {
		machineOpts = append(machineOpts, firecracker.
			WithClient(firecracker.NewClient(machineChroot.SocketPath(), vmmLoggerEntry, true)))
	}

	fcConfig := configs.NewFcConfigProvider(p.jailingFcConfig, p.machineConfig).
		WithHandlersAdapter(p.handlersAdapter).
		WithVethIfaceName(p.vethIfaceName).
		ToSDKConfig()
	m, err := firecracker.NewMachine(ctx, fcConfig, machineOpts...)
	if err != nil {
		return nil, fmt.Errorf("Failed creating machine: %s", err)
	}
	if err := m.Start(ctx); err != nil {
		return nil, fmt.Errorf("Failed to start machine: %v", err)
	}

	return &defaultStartedMachine{
		cniConfig:       p.cniConfig,
		jailingFcConfig: p.jailingFcConfig,
		machineConfig:   p.machineConfig,
		logger:          p.logger,
		machine:         m,
		vethIfaceName:   p.vethIfaceName,
	}, nil
}

func (p *defaultProvider) WithHandlersAdapter(input firecracker.HandlersAdapter) Provider {
	p.handlersAdapter = input
	return p
}

func (p *defaultProvider) WithVethIfaceName(input string) Provider {
	p.vethIfaceName = input
	return p
}
