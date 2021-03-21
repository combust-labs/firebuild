package metadata

import (
	"github.com/combust-labs/firebuild/configs"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/mitchellh/mapstructure"
)

type MMDSLatest struct {
	Latest *MMDSLatestMetadata `json:"latest" mapstructure:"latest"`
}

func (r *MMDSLatest) Serialize() (interface{}, error) {
	output := map[string]interface{}{}
	if err := mapstructure.Decode(r, &output); err != nil {
		return nil, err
	}
	return output, nil
}

type MMDSLatestMetadata struct {
	Metadata *MMDSData `json:"meta-data" mapstructure:"meta-data"`
}

type MMDSData struct {
	VMMID             string                 `json:"vmm-id" mapstructure:"vmm-id"`
	Drives            []models.Drive         `json:"drivers" mapstructure:"drives"`
	Env               map[string]string      `json:"env" mapstructure:"env"`
	Hostname          string                 `json:"hostname" mapstructure:"hostname"`
	MachineConfig     *configs.MachineConfig `json:"machine" mapstructure:"machine"`
	NetworkInterfaces []MDNetworkInterafce   `json:"network-interfaces" mapstructure:"network-interfaces"`
	Rootfs            *MDRootfs              `json:"rootfs" mapstructure:"rootfs"`
	SSHKeys           []string               `json:"ssh-keys" mapstructure:"ssh-keys"`
}
