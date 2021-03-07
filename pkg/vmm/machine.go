package vmm

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/remote"
	"github.com/containernetworking/cni/libcni"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/hashicorp/go-hclog"
	"github.com/pkg/errors"
)

// StartedMachine abstracts a started Firecracker VMM.
type StartedMachine interface {
	// NetworkInterfaces returns network interfaces of a running VMM.
	NetworkInterfaces() firecracker.NetworkInterfaces
	// Stop stops the VMM, remote connected client may be nil.
	Stop(context.Context, remote.ConnectedClient) StoppedOK
	// StopAndWait stops the VMM and waits for the VMM to stop, remote connected client may be nil.
	StopAndWait(context.Context, remote.ConnectedClient)
}

type defaultStartedMachine struct {
	cniConfig     *configs.CNIConfig
	machineConfig *configs.MachineConfig

	logger        hclog.Logger
	machine       *firecracker.Machine
	vethIfaceName string
}

func (m *defaultStartedMachine) NetworkInterfaces() firecracker.NetworkInterfaces {
	return m.machine.Cfg.NetworkInterfaces
}

func (m *defaultStartedMachine) Stop(ctx context.Context, remoteClient remote.ConnectedClient) StoppedOK {

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

func (m *defaultStartedMachine) cleanupCNINetwork() error {
	m.logger.Info("cleaning up CNI network", "vmm-id", m.machine.Cfg.VMID, "iface-name", m.vethIfaceName, "netns", m.machine.Cfg.NetNS)
	cniPlugin := libcni.NewCNIConfigWithCacheDir([]string{m.cniConfig.BinDir}, m.cniConfig.CacheDir, nil)
	networkConfig, err := libcni.LoadConfList(m.cniConfig.ConfDir, m.machineConfig.MachineCNINetworkName)
	if err != nil {
		return errors.Wrap(err, "LoadConfList failed")
	}
	if err := cniPlugin.DelNetworkList(context.Background(), networkConfig, &libcni.RuntimeConf{
		ContainerID: m.machine.Cfg.VMID, // golang firecracker SDK uses the VMID, if VMID is set
		NetNS:       m.machine.Cfg.NetNS,
		IfName:      m.vethIfaceName,
	}); err != nil {
		return errors.Wrap(err, "DelNetworkList failed")
	}

	// clean up the CNI interface directory:
	ifaceCNIDir := filepath.Join(m.cniConfig.CacheDir, m.machine.Cfg.VMID)
	ifaceCNIDirStat, statErr := os.Stat(ifaceCNIDir)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			m.logger.Warn("expected the CNI directory for the CNI interface to be left for manual cleanup but it did not exist",
				"iface-cni-dir", ifaceCNIDir,
				"reason", statErr)
		} else {
			m.logger.Error("failed checking if the CNI directory for the CNI interface exists after cleanup",
				"iface-cni-dir", ifaceCNIDir,
				"reason", statErr)
		}
	}
	if !ifaceCNIDirStat.IsDir() {
		m.logger.Error("CNI directory path points to a file",
			"iface-cni-dir", ifaceCNIDir)
	} else {
		if err := os.RemoveAll(ifaceCNIDir); err != nil {
			m.logger.Error("failed manual CNI directory removal",
				"iface-cni-dir", ifaceCNIDir,
				"reason", err)
		}
	}

	return nil
}
