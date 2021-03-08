package directory

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/combust-labs/firebuild/pkg/naming"
	"github.com/combust-labs/firebuild/pkg/storage"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
)

const providerName = "directory"

type providerConfig struct {
	KernelStorageRoot string `mapstructure:"kernel-storage-root"`
	RootfsStorageRoot string `mapstructure:"rootfs-storage-root"`
}

type provider struct {
	config *providerConfig
	logger hclog.Logger
}

// New returns a new instance of the provider.
func New(logger hclog.Logger) storage.Provider {
	return &provider{
		logger: logger,
	}
}

func (p *provider) Configure(mapConfig map[string]interface{}) error {
	pConfig := &providerConfig{}
	if err := mapstructure.Decode(&mapConfig, pConfig); err != nil {
		p.logger.Error("error when decoding configuration", "reason", err)
		return errors.Wrap(err, "failed decoding provider configuration")
	}
	p.config = pConfig
	return nil
}

// FetchKernel fetches a Linux Kernel by ID.
func (p *provider) FetchKernel(q *storage.KernelLookup) (storage.KernelResult, error) {
	kernelPath := filepath.Join(p.config.KernelStorageRoot, q.ID)
	if _, err := utils.CheckIfExistsAndIsRegular(kernelPath); err != nil {
		return nil, errors.Wrap(err, "failed resolving kernel file")
	}
	// TODO: kernel metadata needs to be implemented
	metadata := map[string]interface{}{}
	return &kernelResult{
		hostPath: kernelPath,
		metadata: metadata,
	}, nil
}

// FetchRootfs fetches a root file system by ID.
func (p *provider) FetchRootfs(q *storage.RootfsLookup) (storage.RootfsResult, error) {
	rootfsPath := filepath.Join(p.config.RootfsStorageRoot,
		strings.ReplaceAll(q.Org, "/", "_"), q.Image, q.Version, naming.RootfsFileName)
	if _, err := utils.CheckIfExistsAndIsRegular(rootfsPath); err != nil {
		return nil, errors.Wrap(err, "failed resolving rootfs file")
	}
	metadata := map[string]interface{}{}
	metadataFilePath := filepath.Join(filepath.Dir(rootfsPath), naming.MetadataFileName)
	if _, err := utils.CheckIfExistsAndIsRegular(metadataFilePath); err == nil {
		metadataFile, err := os.OpenFile(metadataFilePath, os.O_RDONLY, 0664)
		if err != nil {
			return nil, errors.Wrap(err, "failed reading rootfs metadata")
		}
		defer metadataFile.Close()
		if jsonErr := json.NewDecoder(metadataFile).Decode(&metadata); jsonErr != nil {
			return nil, errors.Wrap(err, "failed decoding rootfs metadata")
		}
	}
	return &rootfsResult{
		hostPath: rootfsPath,
		metadata: metadata,
	}, nil
}

func (p *provider) StoreRootfsFile(input *storage.RootfsStore) (*storage.RootfsStoreResult, error) {

	result := &storage.RootfsStoreResult{
		Provider: providerName,
	}

	if err := os.MkdirAll(filepath.Dir(input.LocalPath), 0755); err != nil {
		return nil, errors.Wrap(err, "failed creating target storage directory")
	}

	targetFilePath := filepath.Join(p.config.RootfsStorageRoot,
		strings.ReplaceAll(input.Org, "/", "_"), input.Image, input.Version, naming.RootfsFileName)
	if moveErr := utils.MoveFile(input.LocalPath, targetFilePath); moveErr != nil {
		return nil, errors.Wrap(moveErr, "failed moving source to destination")
	}
	result.RootfsLocation = targetFilePath

	metadataFileName := filepath.Join(filepath.Dir(targetFilePath), naming.MetadataFileName)
	metadataJSONBytes, jsonErr := json.MarshalIndent(&input.Metadata, "", "  ")
	if jsonErr != nil {
		p.logger.Error("Machine metadata could not be serialized to JSON", "metadata", input.Metadata, "reason", jsonErr)
		return result, nil
	}
	if writeErr := ioutil.WriteFile(metadataFileName, metadataJSONBytes, 0755); writeErr != nil {
		p.logger.Error("Machine metadata not written to file", "metadata", input.Metadata, "reason", jsonErr)
		return result, nil
	}
	result.RootfsLocation = metadataFileName

	return result, nil
}
