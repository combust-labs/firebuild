package metadata

import (
	"fmt"
	"strings"

	"github.com/combust-labs/firebuild-mmds/mmds"
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
	CreatedAtUTC int64             `json:"CreatedAtUTC" mapstructure:"CreatedAtUTC"`
	Image        MDImage           `json:"Image" mapstructure:"Image"`
	Labels       map[string]string `json:"Labels" mapstructure:"Labels"`
	Type         Type              `json:"Type" mapstructure:"Type"`
}

// MDImage is the image.
type MDImage struct {
	Org     string `json:"Org" mapstructure:"Org"`
	Image   string `json:"Image" mapstructure:"Image"`
	Version string `json:"Version" mapstructure:"Version"`
}

// MDNetIPConfiguration is the IP configuration of a running VMM.
type MDNetIPConfiguration struct {
	Gateway     string   `json:"Gateway" mapstructure:"Gateway"`
	IfName      string   `json:"IfName" mapstructure:"IfName"`
	IP          string   `json:"IP" mapstructure:"IP"`
	IPAddr      string   `json:"IPAddr" mapstructure:"IPAddr"`
	IPMask      string   `json:"IPMask" mapstructure:"IPMask"`
	IPNet       string   `json:"IPNet" mapstructure:"IPNet"`
	Nameservers []string `json:"NameServers" mapstructure:"NameServers"`
}

// MDNetStaticConfiguration is the static network configuration of a running VMM.
type MDNetStaticConfiguration struct {
	MacAddress string `json:"MacAddress" mapstructure:"MacAddress"`
	// HostDevName is the name of the tap device the VM will use
	HostDeviceName string `json:"HostDeviceName" mapstructure:"HostDeviceName"`
	// IPConfiguration (optional) allows a static IP, gateway and up to 2 DNS nameservers
	// to be automatically configured within the VM upon startup.
	IPConfiguration *MDNetIPConfiguration `json:"IPConfiguration" mapstructure:"IPConfiguration"`
}

// MDNetworkInterafce is network interface configuration of a running VMM.
type MDNetworkInterafce struct {
	AllowMMDS           bool                      `json:"AllowMMDS" mapstructure:"AllowMMDS"`
	InRateLimiter       *models.RateLimiter       `json:"InRateLimiter" mapstructure:"InRateLimiter"`
	OutRateLimiter      *models.RateLimiter       `json:"OutRateLimiter" mapstructure:"OutRateLimiter"`
	StaticConfiguration *MDNetStaticConfiguration `json:"StaticConfiguration" mapstructure:"StaticConfiguration"`
}

// MDRootfsConfig represents the rootfs build configuration.
type MDRootfsConfig struct {
	BuildArgs         map[string]string `json:"BuildArgs" mapstructure:"BuildArgs"`
	Dockerfile        string            `json:"Dockerfile" mapstructure:"Dockerfile"`
	DockerImage       string            `json:"DockerImage" mapstructure:"DockerImage"`
	DockerImageBase   string            `json:"DockerImageBase" mapstructure:"DockerImageBase"`
	PreBuildCommands  []string          `json:"PreBuildCommands" mapstructure:"PreBuildCommands"`
	PostBuildCommands []string          `json:"PostBuildCommands" mapstructure:"PostBuildCommands"`
}

// MDRootfs represents a metadata of the rootfs.
type MDRootfs struct {
	BuildConfig    MDRootfsConfig                 `json:"BuildConfig" mapstructure:"BuildConfig"`
	CreatedAtUTC   int64                          `json:"CreatedAtUTC" mapstructure:"CreatedAtUTC"`
	EntrypointInfo *mmds.MMDSRootfsEntrypointInfo `json:"EntrypointInfo" mapstructure:"EntrypointInfo"`
	Image          MDImage                        `json:"Image" mapstructure:"Image"`
	Labels         map[string]string              `json:"Labels" mapstructure:"Labels"`
	Parent         interface{}                    `json:"Parent" mapstructure:"Parent"`
	Ports          []string                       `json:"Ports" mapstructure:"Ports"`
	Tag            string                         `json:"Tag" mapstructure:"Tag"`
	Type           Type                           `json:"Type" mapstructure:"Type"`
	Volumes        []string                       `json:"Volumes" mapstructure:"Volumes"`
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
	CNI       *configs.CNIConfig                `json:"CNI" mapstructure:"CNI"`
	Jailer    *configs.JailingFirecrackerConfig `json:"Jailer" mapstructure:"Jailer"`
	Machine   *configs.MachineConfig            `json:"Machine" mapstructure:"Machine"`
	RunConfig *configs.RunCommandConfig         `json:"RunConfig" mapstructure:"RunConfig"`
}

// MDRunCNI represents the CNI metadata of a running VMM.
// This metadata is stored in the VMM run cache directory.
type MDRunCNI struct {
	VethName string `json:"VethName" mapstructure:"VethName"`
	NetName  string `json:"NetName" mapstructure:"NetName"`
	NetNS    string `json:"NetNS" mapstructure:"NetNS"`
}

// MDRun contains the runtime information about a VMM.
type MDRun struct {
	Bootstrap         *mmds.MMDSBootstrap  `json:"Bootstrap,omitempty" mapstructure:"Bootstrap,omitempty"`
	CNI               MDRunCNI             `json:"CNI" mapstructure:"CNI"`
	Configs           MDRunConfigs         `json:"Configs" mapstructure:"Configs"`
	Drives            []models.Drive       `json:"Drivers" mapstructure:"Drives"`
	NetworkInterfaces []MDNetworkInterafce `json:"NetworkInterfaces" mapstructure:"NetworkInterfaces"`
	PID               pid.RunningVMMPID    `json:"Pid" mapstructure:"Pid"`
	Rootfs            *MDRootfs            `json:"Rootfs" mapstructure:"Rootfs"`
	RunCache          string               `json:"RunCache" mapstructure:"RunCache"`
	StartedAtUTC      int64                `json:"StartedAtUTC" mapstructure:"StartedAtUTC"`
	VMMID             string               `json:"VMMID" mapstructure:"VMMID"`
	Type              Type                 `json:"Type" mapstructure:"Type"`
}

// AsMMDS converts the run metadata to MMDS metadata.
func (r *MDRun) AsMMDS() (interface{}, error) {

	env, err := r.Configs.RunConfig.MergedEnvironment()
	if err != nil {
		return nil, errors.Wrap(err, "failed fetching merged env")
	}
	keys, err := r.Configs.RunConfig.PublicKeys()
	if err != nil {
		return nil, errors.Wrap(err, "failed fetching public keys")
	}

	entrypointInfo := &mmds.MMDSRootfsEntrypointInfo{
		Cmd:        r.Rootfs.EntrypointInfo.Cmd,
		Entrypoint: r.Rootfs.EntrypointInfo.Entrypoint,
		Env:        r.Rootfs.EntrypointInfo.Env,
		Shell:      r.Rootfs.EntrypointInfo.Shell,
		User:       r.Rootfs.EntrypointInfo.User,
		Workdir:    r.Rootfs.EntrypointInfo.Workdir,
	}
	if len(r.Configs.RunConfig.CapturedCmd()) > 0 {
		entrypointInfo.Cmd = r.Configs.RunConfig.CapturedCmd()
	}

	entrypointJSON, err := entrypointInfo.ToJsonString()
	if err != nil {
		return nil, errors.Wrap(err, "failed fetching public keys")
	}

	metadata := &mmds.MMDSLatest{
		Latest: &mmds.MMDSLatestMetadata{
			Metadata: &mmds.MMDSData{
				Bootstrap: r.Bootstrap,
				VMMID:     r.VMMID,
				Drives: func() map[string]*mmds.MMDSDrive {
					result := map[string]*mmds.MMDSDrive{}
					for _, drive := range r.Drives {
						result[*drive.DriveID] = &mmds.MMDSDrive{
							DriveID:      *drive.DriveID,
							IsReadOnly:   fmt.Sprintf("%v", *drive.IsReadOnly),
							IsRootDevice: fmt.Sprintf("%v", *drive.IsRootDevice),
							Partuuid:     drive.Partuuid,
							PathOnHost:   *drive.PathOnHost,
						}
					}
					return result
				}(),
				EntrypointJSON: entrypointJSON,
				Env:            env,
				LocalHostname:  r.Configs.RunConfig.Hostname,
				Machine: &mmds.MMDSMachine{
					CPU:         fmt.Sprintf("%d", r.Configs.Machine.CPU),
					CPUTemplate: r.Configs.Machine.CPUTemplate,
					HTEnabled:   fmt.Sprintf("%v", r.Configs.Machine.HTEnabled),
					KernelArgs:  r.Configs.Machine.KernelArgs,
					Mem:         fmt.Sprintf("%d", r.Configs.Machine.Mem),
					VMLinuxID:   r.Configs.Machine.VMLinuxID,
				},
				Network: &mmds.MMDSNetwork{
					CNINetworkName: r.Configs.Machine.CNINetworkName,
					Interfaces: func() map[string]*mmds.MMDSNetworkInterface {
						result := map[string]*mmds.MMDSNetworkInterface{}
						for _, nic := range r.NetworkInterfaces {
							result[nic.StaticConfiguration.MacAddress] = &mmds.MMDSNetworkInterface{
								HostDeviceName: nic.StaticConfiguration.HostDeviceName,
								Gateway:        nic.StaticConfiguration.IPConfiguration.Gateway,
								IfName:         nic.StaticConfiguration.IPConfiguration.IfName,
								IP:             nic.StaticConfiguration.IPConfiguration.IP,
								IPAddr:         nic.StaticConfiguration.IPConfiguration.IPAddr,
								IPMask:         nic.StaticConfiguration.IPConfiguration.IPMask,
								IPNet:          nic.StaticConfiguration.IPConfiguration.IPNet,
								Nameservers:    strings.Join(nic.StaticConfiguration.IPConfiguration.Nameservers, ","),
							}
						}
						return result
					}(),
				},
				ImageTag: r.Rootfs.Tag,
				Users: func() map[string]*mmds.MMDSUser {
					result := map[string]*mmds.MMDSUser{}
					if r.Configs.Machine.SSHUser != "" {
						result[r.Configs.Machine.SSHUser] = &mmds.MMDSUser{
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
		HostDeviceName:  sc.HostDevName,
		IPConfiguration: fcIPConfiguration(sc.IPConfiguration),
	}
}
