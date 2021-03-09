package cni

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/utils"
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

// RunningVMMCNIMetadata represents the CNI metadata of a running VMM.
// This metadata is stored in the VMM run cache directory.
type RunningVMMCNIMetadata struct {
	Config        *configs.CNIConfig `json:"config"`
	VethIfaceName string             `json:"veth-iface-name"`
	NetName       string             `json:"net-name"`
	NetNS         string             `json:"net-ns"`
}

// FetchCNIMetadataIfExists fetches the metadata from a file in the required directory, if the file exists.
// Returns a metadata, if file exists, a boolean indicating if file existed and an error,
// if metadata lookup went wrong.
func FetchCNIMetadataIfExists(cacheDirectory string) (*RunningVMMCNIMetadata, bool, error) {
	metadataFile := filepath.Join(cacheDirectory, "cni")
	if _, err := utils.CheckIfExistsAndIsRegular(metadataFile); err != nil {
		if !os.IsNotExist(err) {
			return nil, false, err
		}
		if os.IsNotExist(err) {
			return nil, false, nil
		}
	}
	pidJSONBytes, err := ioutil.ReadFile(metadataFile)
	if err != nil {
		return nil, false, err
	}
	result := &RunningVMMCNIMetadata{}
	if jsonErr := json.Unmarshal(pidJSONBytes, result); jsonErr != nil {
		return nil, false, jsonErr
	}
	return result, true, nil
}

// WriteCNIMetadataToFile writes a metadata to a cni file under a directory.
func WriteCNIMetadataToFile(cacheDirectory string, metadata *RunningVMMCNIMetadata) error {
	metadataBytes, jsonErr := json.Marshal(metadata)
	if jsonErr != nil {
		return errors.Wrap(jsonErr, "failed serializing CNI metadata to JSON")
	}
	if err := ioutil.WriteFile(filepath.Join(cacheDirectory, "cni"), []byte(metadataBytes), 0644); err != nil {
		return errors.Wrap(err, "failed writing PID metadata the cache file")
	}
	return nil
}
