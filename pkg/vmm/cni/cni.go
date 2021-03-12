package cni

import (
	"context"
	"os"
	"path/filepath"

	"github.com/combust-labs/firebuild/configs"
	"github.com/containernetworking/cni/libcni"
	"github.com/hashicorp/go-hclog"
	"github.com/pkg/errors"
)

// CleanupCNI removes the CNI interface from the network and cleans up the CNI cache directory.
func CleanupCNI(logger hclog.Logger, cniConfig *configs.CNIConfig, vmmID, vethIfaceName, netName, netNS string) error {
	logger.Info("cleaning up CNI network", "vmm-id", vmmID, "iface-name", vethIfaceName, "netns", netNS)
	cniPlugin := libcni.NewCNIConfigWithCacheDir([]string{cniConfig.BinDir}, cniConfig.CacheDir, nil)
	networkConfig, err := libcni.LoadConfList(cniConfig.ConfDir, netName)
	if err != nil {
		return errors.Wrap(err, "LoadConfList failed")
	}
	if err := cniPlugin.DelNetworkList(context.Background(), networkConfig, &libcni.RuntimeConf{
		ContainerID: vmmID, // golang firecracker SDK uses the VMID, if VMID is set
		NetNS:       netNS,
		IfName:      vethIfaceName,
	}); err != nil {
		return errors.Wrap(err, "DelNetworkList failed")
	}

	// clean up the CNI interface directory:
	ifaceCNIDir := filepath.Join(cniConfig.CacheDir, vmmID)
	ifaceCNIDirStat, statErr := os.Stat(ifaceCNIDir)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			logger.Warn("expected the CNI directory for the CNI interface to be left for manual cleanup but it did not exist",
				"iface-cni-dir", ifaceCNIDir,
				"reason", statErr)
		} else {
			logger.Error("failed checking if the CNI directory for the CNI interface exists after cleanup",
				"iface-cni-dir", ifaceCNIDir,
				"reason", statErr)
		}
	}
	if !ifaceCNIDirStat.IsDir() {
		logger.Error("CNI directory path points to a file",
			"iface-cni-dir", ifaceCNIDir)
	} else {
		if err := os.RemoveAll(ifaceCNIDir); err != nil {
			logger.Error("failed manual CNI directory removal",
				"iface-cni-dir", ifaceCNIDir,
				"reason", err)
		} else {
			logger.Info("manual CNI directory removal successful",
				"iface-cni-dir", ifaceCNIDir)
		}
	}

	return nil
}
