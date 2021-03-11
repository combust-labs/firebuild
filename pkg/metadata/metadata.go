package metadata

import (
	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/vmm/cni"
	"github.com/combust-labs/firebuild/pkg/vmm/pid"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
)

type MetadataType = string

const (
	MetadataTypeBaseOS   = MetadataType("baseos")
	MetadataTypeRootfsOS = MetadataType("rootfs")
	MetadataTypeRun      = MetadataType("run")
)

// MDBaseOS is the base OS metadata.
type MDBaseOS struct {
	CreatedAtUTC int64             `json:"created-at-utc"`
	Image        MDImage           `json:"image"`
	Labels       map[string]string `json:"labels"`
	Type         MetadataType      `json:"type"`
}

// MDImage is the image.
type MDImage struct {
	Org     string `json:"org"`
	Image   string `json:"image"`
	Version string `json:"version"`
}

// MDNetIPConfiguration is the IP configuration of a running VMM.
type MDNetIPConfiguration struct {
	IPAddr      string
	Gateway     string
	Nameservers []string
	IfName      string
}

// MDNetStaticConfiguration is the static network configuration of a running VMM.
type MDNetStaticConfiguration struct {
	MacAddress string `json:"mac-address"`
	// HostDevName is the name of the tap device the VM will use
	HostDevName string `json:"host-dev-name"`
	// IPConfiguration (optional) allows a static IP, gateway and up to 2 DNS nameservers
	// to be automatically configured within the VM upon startup.
	IPConfiguration *MDNetIPConfiguration
}

// MDNetworkInterafce is network interface configuration of a running VMM.
type MDNetworkInterafce struct {
	AllowMMDS           bool                      `json:"allow-mmds"`
	InRateLimiter       *models.RateLimiter       `json:"in-rate-limiter"`
	OutRateLimiter      *models.RateLimiter       `json:"out-rate-limiter"`
	StaticConfiguration *MDNetStaticConfiguration `json:"static-configuration"`
}

// MDRootfs represents a metadata of the rootfs.
type MDRootfs struct {
	BuildArgs    map[string]string `json:"build-args"`
	CreatedAtUTC int64             `json:"created-at-utc"`
	Image        MDImage           `json:"image"`
	Parent       interface{}       `json:"parent"`
	Tag          string            `json:"tag"`
	Type         MetadataType      `json:"type"`
}

// MDRunConfigs contains the configuration of the running VMM.
type MDRunConfigs struct {
	CNI      configs.CNIConfig                `json:"cni"`
	Jailer   configs.JailingFirecrackerConfig `json:"jailer"`
	Machine  configs.MachineConfig            `json:"machine"`
	RunCache configs.RunCacheConfig           `json:"run-cache"`
}

// MDRun contains the runtime information about a VMM.
type MDRun struct {
	CNI               cni.RunningVMMCNIMetadata `json:"cni"`
	Configs           MDRunConfigs              `json:"configs"`
	Drives            []models.Drive            `json:"drivers"`
	NetworkInterfaces []MDNetworkInterafce      `json:"network-interfaces"`
	PID               pid.RunningVMMPID         `json:"pid"`
	StartedAtUTC      int64                     `json:"started-at-utc"`
	VMMID             string                    `json:"vmm-id"`
	Type              MetadataType              `json:"type"`
}
