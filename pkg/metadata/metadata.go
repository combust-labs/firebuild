package metadata

import (
	"fmt"
	"strings"

	"github.com/combust-labs/firebuild/configs"
	"github.com/combust-labs/firebuild/pkg/utils"
	"github.com/combust-labs/firebuild/pkg/vmm/pid"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
)

// Type is the type of the metadata entry stored in a file.
type Type = string

// Metadata types.
const (
	MetadataTypeBaseOS = Type("baseos")
	MetadataTypeRootfs = Type("rootfs")
	MetadataTypeRun    = Type("run")
)

// MDBaseOS is the base OS metadata.
type MDBaseOS struct {
	CreatedAtUTC int64             `json:"created-at-utc" mapstructure:"created-at-utc"`
	Image        MDImage           `json:"image" mapstructure:"image"`
	Labels       map[string]string `json:"labels" mapstructure:"labels"`
	Type         Type              `json:"type" mapstructure:"type"`
}

// MDImage is the image.
type MDImage struct {
	Org     string `json:"org" mapstructure:"org"`
	Image   string `json:"image" mapstructure:"image"`
	Version string `json:"version" mapstructure:"version"`
}

// MDNetIPConfiguration is the IP configuration of a running VMM.
type MDNetIPConfiguration struct {
	Gateway     string   `json:"gateway" mapstructure:"gateway"`
	IfName      string   `json:"ifname" mapstructure:"ifname"`
	IP          string   `json:"ip" mapstructure:"ip"`
	IPAddr      string   `json:"ip-addr" mapstructure:"ip-addr"`
	IPMask      string   `json:"ip-mask" mapstructure:"ip-mask"`
	IPNet       string   `json:"ip-net" mapstructure:"ip-net"`
	Nameservers []string `json:"nameservers" mapstructure:"nameservers"`
}

// MDNetStaticConfiguration is the static network configuration of a running VMM.
type MDNetStaticConfiguration struct {
	MacAddress string `json:"mac-address" mapstructure:"mac-address"`
	// HostDevName is the name of the tap device the VM will use
	HostDevName string `json:"host-dev-name" mapstructure:"host-dev-name"`
	// IPConfiguration (optional) allows a static IP, gateway and up to 2 DNS nameservers
	// to be automatically configured within the VM upon startup.
	IPConfiguration *MDNetIPConfiguration `json:"ip-configuration" mapstructure:"ip-configuration"`
}

// MDNetworkInterafce is network interface configuration of a running VMM.
type MDNetworkInterafce struct {
	AllowMMDS           bool                      `json:"allow-mmds" mapstructure:"allow-mmds"`
	InRateLimiter       *models.RateLimiter       `json:"in-rate-limiter" mapstructure:"in-rate-limiter"`
	OutRateLimiter      *models.RateLimiter       `json:"out-rate-limiter" mapstructure:"out-rate-limiter"`
	StaticConfiguration *MDNetStaticConfiguration `json:"static-configuration" mapstructure:"static-configuration"`
}

// MDRootfsConfig represents the rootfs build configuration.
type MDRootfsConfig struct {
	BuildArgs         map[string]string `json:"build-args" mapstructure:"build-args"`
	Dockerfile        string            `json:"dockerfile" mapstructure:"dockerfile"`
	PreBuildCommands  []string          `json:"pre-build-commands" mapstructure:"pre-build-commands"`
	PostBuildCommands []string          `json:"post-build-commands" mapstructure:"post-build-commands"`
}

// MDRootfs represents a metadata of the rootfs.
type MDRootfs struct {
	BuildConfig  MDRootfsConfig    `json:"build-config" mapstructure:"build-config"`
	CreatedAtUTC int64             `json:"created-at-utc" mapstructure:"created-at-utc"`
	Image        MDImage           `json:"image" mapstructure:"image"`
	Labels       map[string]string `json:"labels" mapstructure:"labels"`
	Parent       interface{}       `json:"parent" mapstructure:"parent"`
	Ports        []string          `json:"ports" mapstructure:"ports"`
	Tag          string            `json:"tag" mapstructure:"tag"`
	Type         Type              `json:"type" mapstructure:"type"`
	Volumes      []string          `json:"volumes" mapstructure:"volumes"`
}

// MDRootfsFromInterface unwraps an interface{} as *MDRootfs.
func MDRootfsFromInterface(input interface{}) (*MDRootfs, error) {
	mdrootfs := &MDRootfs{}
	if err := mapstructure.Decode(input, mdrootfs); err != nil {
		return nil, errors.Wrap(err, "failed decoding mdrun")
	}
	return mdrootfs, nil
}

// MDRunConfigs contains the configuration of the running VMM.
type MDRunConfigs struct {
	CNI       *configs.CNIConfig                `json:"cni" mapstructure:"cni"`
	Jailer    *configs.JailingFirecrackerConfig `json:"jailer" mapstructure:"jailer"`
	Machine   *configs.MachineConfig            `json:"machine" mapstructure:"machine"`
	RunConfig *configs.RunCommandConfig         `json:"run-config" mapstructure:"run-config"`
}

// MDRunCNI represents the CNI metadata of a running VMM.
// This metadata is stored in the VMM run cache directory.
type MDRunCNI struct {
	VethIfaceName string `json:"veth-iface-name" mapstructure:"veth-iface-name"`
	NetName       string `json:"net-name" mapstructure:"net-name"`
	NetNS         string `json:"net-ns" mapstructure:"net-ns"`
}

// MDRun contains the runtime information about a VMM.
type MDRun struct {
	CNI               MDRunCNI             `json:"cni" mapstructure:"cni"`
	Configs           MDRunConfigs         `json:"configs" mapstructure:"configs"`
	Drives            []models.Drive       `json:"drivers" mapstructure:"drives"`
	NetworkInterfaces []MDNetworkInterafce `json:"network-interfaces" mapstructure:"network-interfaces"`
	PID               pid.RunningVMMPID    `json:"pid" mapstructure:"pid"`
	Rootfs            *MDRootfs            `json:"rootfs" mapstructure:"rootfs"`
	RunCache          string               `json:"run-cache" mapstructure:"run-cache"`
	StartedAtUTC      int64                `json:"started-at-utc" mapstructure:"started-at-utc"`
	VMMID             string               `json:"vmm-id" mapstructure:"vmm-id"`
	Type              Type                 `json:"type" mapstructure:"type"`
}

func (r *MDRun) AsMMDS() (interface{}, error) {

	env, err := r.Configs.RunConfig.MergedEnvironment()
	if err != nil {
		return nil, errors.Wrap(err, "failed fetching merged env")
	}
	keys, err := r.Configs.RunConfig.PublicKeys()
	if err != nil {
		return nil, errors.Wrap(err, "failed fetching public keys")
	}

	metadata := &MMDSLatest{
		Latest: &MMDSLatestMetadata{
			Metadata: &MMDSData{
				VMMID: r.VMMID,
				Drives: func() map[string]*MMDSDrive {
					result := map[string]*MMDSDrive{}
					for _, drive := range r.Drives {
						result[*drive.DriveID] = &MMDSDrive{
							DriveID:      *drive.DriveID,
							IsReadOnly:   fmt.Sprintf("%v", *drive.IsReadOnly),
							IsRootDevice: fmt.Sprintf("%v", *drive.IsRootDevice),
							Partuuid:     drive.Partuuid,
							PathOnHost:   *drive.PathOnHost,
						}
					}
					return result
				}(),
				Env:           env,
				LocalHostname: r.Configs.RunConfig.Hostname,
				Machine: &MMDSMachine{
					CPU:         fmt.Sprintf("%d", r.Configs.Machine.CPU),
					CPUTemplate: r.Configs.Machine.CPUTemplate,
					HTEnabled:   fmt.Sprintf("%v", r.Configs.Machine.HTEnabled),
					KernelArgs:  r.Configs.Machine.KernelArgs,
					Mem:         fmt.Sprintf("%d", r.Configs.Machine.Mem),
					VMLinuxID:   r.Configs.Machine.VMLinuxID,
				},
				Network: &MMDSNetwork{
					CNINetworkName: r.Configs.Machine.CNINetworkName,
					Interfaces: func() map[string]*MMDSNetworkInterface {
						result := map[string]*MMDSNetworkInterface{}
						for _, nic := range r.NetworkInterfaces {
							result[nic.StaticConfiguration.MacAddress] = &MMDSNetworkInterface{
								HostDevName: nic.StaticConfiguration.HostDevName,
								Gateway:     nic.StaticConfiguration.IPConfiguration.Gateway,
								IfName:      nic.StaticConfiguration.IPConfiguration.IfName,
								IP:          nic.StaticConfiguration.IPConfiguration.IP,
								IPAddr:      nic.StaticConfiguration.IPConfiguration.IPAddr,
								IPMask:      nic.StaticConfiguration.IPConfiguration.IPMask,
								IPNet:       nic.StaticConfiguration.IPConfiguration.IPNet,
								Nameservers: strings.Join(nic.StaticConfiguration.IPConfiguration.Nameservers, ","),
							}
						}
						return result
					}(),
					SSHPort: fmt.Sprintf("%d", r.Configs.Machine.SSHPort),
				},
				ImageTag: r.Rootfs.Tag,
				Users: func() map[string]*MMDSUser {
					result := map[string]*MMDSUser{}
					if r.Configs.Machine.SSHUser != "" {
						result[r.Configs.Machine.SSHUser] = &MMDSUser{
							SSHKeys: func() string {
								resp := []string{}
								for _, key := range keys {
									resp = append(resp, string(utils.MarshalSSHPublicKey(key)))
								}
								return strings.Join(resp, "\n")
							}(),
						}
					}
					return result
				}(),
			},
		},
	}
	return metadata.Serialize()
}

// FcNetworkInterfacesToMetadata converts firecracker network interfaces to the metadata network interfaces.
func FcNetworkInterfacesToMetadata(nifs firecracker.NetworkInterfaces) []MDNetworkInterafce {
	response := []MDNetworkInterafce{}
	for _, nif := range nifs {
		response = append(response, fcNetworkInterface(nif))
	}
	return response
}

func fcNetworkInterface(nif firecracker.NetworkInterface) MDNetworkInterafce {
	return MDNetworkInterafce{
		AllowMMDS:           nif.AllowMMDS,
		StaticConfiguration: fcStaticConfiguration(nif.StaticConfiguration),
		InRateLimiter:       nif.InRateLimiter,
		OutRateLimiter:      nif.OutRateLimiter,
	}
}

func fcIPConfiguration(ipc *firecracker.IPConfiguration) *MDNetIPConfiguration {
	if ipc == nil {
		return nil
	}
	return &MDNetIPConfiguration{
		IP:     ipc.IPAddr.IP.String(),
		IPAddr: ipc.IPAddr.String(),
		IPMask: ipc.IPAddr.Mask.String(),
		IPNet:  ipc.IPAddr.Network(),
		Gateway: func() string {
			if ipc.Gateway != nil {
				return ipc.Gateway.String()
			}
			return ""
		}(),
		IfName:      ipc.IfName,
		Nameservers: ipc.Nameservers,
	}
}

func fcStaticConfiguration(sc *firecracker.StaticNetworkConfiguration) *MDNetStaticConfiguration {
	if sc == nil {
		return nil
	}
	return &MDNetStaticConfiguration{
		MacAddress:      sc.MacAddress,
		HostDevName:     sc.HostDevName,
		IPConfiguration: fcIPConfiguration(sc.IPConfiguration),
	}
}
