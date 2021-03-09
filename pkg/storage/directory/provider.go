package directory

import (
	"encoding/json"
	"fmt"
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
	p.logger.Debug("configuring storage provider")
	pConfig := &providerConfig{}
	if err := mapstructure.Decode(&mapConfig, pConfig); err != nil {
		p.logger.Error("error when decoding configuration", "reason", err)
		return errors.Wrap(err, "failed decoding provider configuration")
	}
	p.config = pConfig
	p.logger.Debug("storage provider configured")
	return nil
}

// FetchKernel fetches a Linux Kernel by ID.
func (p *provider) FetchKernel(q *storage.KernelLookup) (storage.KernelResult, error) {
	p.logger.Debug("looking up kernel", "kernel-id", q.ID)
	kernelPath := filepath.Join(p.config.KernelStorageRoot, q.ID)
	if _, err := utils.CheckIfExistsAndIsRegular(kernelPath); err != nil {
		p.logger.Error("error looking up kernel", "reason", err, "kernel-id", q.ID)
		return nil, errors.Wrap(err, "failed resolving kernel file")
	}
	// TODO: kernel metadata needs to be implemented
	p.logger.Debug("kernel located", "kernel-id", q.ID)
	metadata := map[string]interface{}{}
	return &kernelResult{
		hostPath: kernelPath,
		metadata: metadata,
	}, nil
}

// FetchRootfs fetches a root file system by ID.
func (p *provider) FetchRootfs(q *storage.RootfsLookup) (storage.RootfsResult, error) {
	rootfsID := fmt.Sprintf("%s/%s:%s", q.Org, q.Image, q.Version)
	p.logger.Debug("looking up rootfs", "rootfs-id", rootfsID)
	rootfsPath := filepath.Join(p.config.RootfsStorageRoot,
		strings.ReplaceAll(q.Org, "/", "_"), q.Image, q.Version, naming.RootfsFileName)
	if _, err := utils.CheckIfExistsAndIsRegular(rootfsPath); err != nil {
		p.logger.Error("error looking up rootfs", "reason", err, "rootfs-id", rootfsID)
		return nil, errors.Wrap(err, "failed resolving rootfs file")
	}
	metadata := map[string]interface{}{}
	metadataFilePath := filepath.Join(filepath.Dir(rootfsPath), naming.MetadataFileName)
	hasMetadata := true
	if _, err := utils.CheckIfExistsAndIsRegular(metadataFilePath); err != nil {
		if os.IsNotExist(err) {
			hasMetadata = false
		} else {
			p.logger.Error("error looking up rootfs metadata", "reason", err, "rootfs-id", rootfsID, "===============", os.IsNotExist(err))
			return nil, err
		}
	}
	if hasMetadata {
		metadataFile, err := os.OpenFile(metadataFilePath, os.O_RDONLY, 0664)
		if err != nil {
			p.logger.Error("error opening rootfs metadata", "reason", err, "rootfs-id", rootfsID, "metadata-path", metadataFile)
			return nil, errors.Wrap(err, "failed reading rootfs metadata")
		}
		defer metadataFile.Close()
		if jsonErr := json.NewDecoder(metadataFile).Decode(&metadata); jsonErr != nil {
			p.logger.Error("error reading rootfs metadata as JSON", "reason", err, "rootfs-id", rootfsID, "metadata-path", metadataFile)
			return nil, errors.Wrap(err, "failed decoding rootfs metadata")
		}
	} else {
		p.logger.Debug("rootfs without metadata", "rootfs-id", rootfsID)
	}
	p.logger.Debug("rootfs located", "rootfs-id", rootfsID)
	return &rootfsResult{
		hostPath: rootfsPath,
		metadata: metadata,
	}, nil
}

func (p *provider) StoreRootfsFile(input *storage.RootfsStore) (*storage.RootfsStoreResult, error) {
	rootfsID := fmt.Sprintf("%s/%s:%s", input.Org, input.Image, input.Version)
	result := &storage.RootfsStoreResult{
		Provider: providerName,
	}

	p.logger.Debug("storing rootfs", "rootfs-id", rootfsID)

	targetFilePath := filepath.Join(p.config.RootfsStorageRoot,
		strings.ReplaceAll(input.Org, "/", "_"), input.Image, input.Version, naming.RootfsFileName)
	p.logger.Debug("ensuring rootfs parent directory exists", "rootfs-id", rootfsID, "directory", filepath.Dir(targetFilePath))
	if err := os.MkdirAll(filepath.Dir(targetFilePath), 0755); err != nil {
		p.logger.Error("error creating rootfs parent directory", "reason", err, "rootfs-id", rootfsID)
		return nil, errors.Wrap(err, "failed creating target storage directory")
	}
	p.logger.Debug("moving rootfs", "rootfs-id", rootfsID,
		"source", input.LocalPath,
		"target", targetFilePath)
	if moveErr := utils.MoveFile(input.LocalPath, targetFilePath); moveErr != nil {
		p.logger.Error("error moving rootfs", "reason", moveErr, "rootfs-id", rootfsID)
		return nil, errors.Wrap(moveErr, "failed moving source to destination")
	}
	result.RootfsLocation = targetFilePath

	p.logger.Debug("writing rootfs metadata", "rootfs-id", rootfsID)
	metadataFileName := filepath.Join(filepath.Dir(targetFilePath), naming.MetadataFileName)
	metadataJSONBytes, jsonErr := json.MarshalIndent(&input.Metadata, "", "  ")
	if jsonErr != nil {
		p.logger.Error("error serialzing rootfs metadata to JSON", "reason", jsonErr, "rootfs-id", rootfsID)
		return result, nil
	}
	if writeErr := ioutil.WriteFile(metadataFileName, metadataJSONBytes, 0755); writeErr != nil {
		p.logger.Error("error writing rootfs metadata to file", "reason", writeErr, "rootfs-id", rootfsID)
		return result, nil
	}
	result.MetadataLocation = metadataFileName

	p.logger.Debug("rootfs stored", "rootfs-id", rootfsID)

	return result, nil
}
