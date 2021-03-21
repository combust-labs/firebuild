package vmm

import (
	"context"
	"sync"
	"time"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/metadata"
	"github.com/combust-labs/firebuild/pkg/remote"
	"github.com/combust-labs/firebuild/pkg/vmm/cni"
	"github.com/combust-labs/firebuild/pkg/vmm/pid"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/hashicorp/go-hclog"
	"github.com/pkg/errors"
)

// StartedMachine abstracts a started Firecracker VMM.
type StartedMachine interface {
	// Cleanup handles cleanup when the machine is stopped from outside of the controlling process.
	Cleanup(chan bool)
	// Decorates metadata with additional properties.
	DecorateMetadata(*metadata.MDRun) error
	// Stop stops the VMM, remote connected client may be nil.
	Stop(context.Context, remote.ConnectedClient) StoppedOK
	// StopAndWait stops the VMM and waits for the VMM to stop, remote connected client may be nil.
	StopAndWait(context.Context, remote.ConnectedClient)
	// Wait awaits for the VMM exit.
	Wait(context.Context)
}

type defaultStartedMachine struct {
	sync.Mutex

	cniConfig       *configs.CNIConfig
	jailingFcConfig *configs.JailingFirecrackerConfig
	machineConfig   *configs.MachineConfig

	logger        hclog.Logger
	machine       *firecracker.Machine
	vethIfaceName string

	wasStopped bool
}

func (m *defaultStartedMachine) Cleanup(c chan bool) {
	m.Lock()
	defer m.Unlock()
	if !m.wasStopped {
		m.cleanupCNINetwork()
		// only handle the channel if the VMM wasn't stopped manually
		c <- StoppedGracefully
	}
}

func (m *defaultStartedMachine) DecorateMetadata(md *metadata.MDRun) error {
	machinePid, err := m.machine.PID()
	if err != nil {
		return errors.Wrap(err, "machine pid read")
	}
	md.PID = pid.RunningVMMPID{Pid: machinePid}
	return nil
}

func (m *defaultStartedMachine) Stop(ctx context.Context, remoteClient remote.ConnectedClient) StoppedOK {
	m.Lock()
	defer m.Unlock()

	if !m.wasStopped {
		m.wasStopped = true
	} else {
		return StoppedGracefully
	}

	if remoteClient != nil {
		m.logger.Info("Closing remote client...")
		if err := remoteClient.Close(); err != nil {
			m.logger.Info("Remote client closed", "error", err)
		}
	}

	shutdownCtx, cancelFunc := context.WithTimeout(ctx, time.Second*time.Duration(m.machineConfig.ShutdownGracefulTimeoutSeconds))
	defer cancelFunc()

	m.logger.Info("Attempting VMM graceful shutdown...")

	chanStopped := make(chan error, 1)
	go func() {
		// Ask the machine to shut down so the file system gets flushed
		// and out changes are written to disk.
		chanStopped <- m.machine.Shutdown(shutdownCtx)
	}()

	stoppedState := StoppedForcefully

	select {
	case stopErr := <-chanStopped:
		if stopErr != nil {
			m.logger.Warn("VMM stopped with error but within timeout", "reason", stopErr)
			m.logger.Warn("VMM stopped forcefully", "error", m.machine.StopVMM())
		} else {
			m.logger.Warn("VMM stopped gracefully")
			stoppedState = StoppedGracefully
		}
	case <-shutdownCtx.Done():
		m.logger.Warn("VMM failed to stop gracefully: timeout reached")
		m.logger.Warn("VMM stopped forcefully", "error", m.machine.StopVMM())
	}

	m.logger.Info("Cleaning up CNI network...")

	cniCleanupErr := m.cleanupCNINetwork()

	m.logger.Info("CNI network cleanup status", "error", cniCleanupErr)

	return stoppedState
}

func (m *defaultStartedMachine) StopAndWait(ctx context.Context, remoteClient remote.ConnectedClient) {
	go func() {
		if m.Stop(ctx, remoteClient) == StoppedForcefully {
			m.logger.Warn("Machine was not stopped gracefully, see previous errors. It's possible that the file system may not be complete. Retry or proceed with caution.")
		}
	}()
	m.logger.Info("Waiting for machine to stop...")
	m.machine.Wait(ctx)
}

func (m *defaultStartedMachine) Wait(ctx context.Context) {
	m.logger.Info("Waiting for machine to stop...")
	m.machine.Wait(ctx)
}

func (m *defaultStartedMachine) cleanupCNINetwork() error {
	return cni.CleanupCNI(m.logger, m.cniConfig,
		m.machine.Cfg.VMID,
		m.vethIfaceName,
		m.machineConfig.CNINetworkName,
		m.machine.Cfg.NetNS)
}
