package strategy

import (
	"context"
	"time"

	"github.com/combust-labs/firebuild/pkg/metadata"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/hashicorp/go-hclog"
)

// Handler names
const (
	MetadataExtractorName = "fcinit.MetadataExtractor"
)

// NewPseudoCloudInitHandler returns a firecracker handler which can be used to inject state into
// a virtual machine file system prior to start.
func NewMetadataExtractorHandler(logger hclog.Logger, md *metadata.MDRun) firecracker.Handler {
	return firecracker.Handler{
		Name: MetadataExtractorName,
		Fn: func(ctx context.Context, m *firecracker.Machine) error {

			var cniIface *firecracker.NetworkInterface
			for idx, iface := range m.Cfg.NetworkInterfaces {
				if iface.CNIConfiguration != nil {
					cniIface = &m.Cfg.NetworkInterfaces[idx]
					break
				}
			}

			setMetadata := false

			if cniIface != nil {
				md.CNI = metadata.MDRunCNI{
					NetName:       cniIface.CNIConfiguration.NetworkName,
					NetNS:         m.Cfg.NetNS,
					VethIfaceName: cniIface.CNIConfiguration.IfName,
				}
				setMetadata = cniIface.AllowMMDS
			}

			md.StartedAtUTC = time.Now().UTC().Unix()
			md.Drives = m.Cfg.Drives
			md.NetworkInterfaces = metadata.FcNetworkInterfacesToMetadata(m.Cfg.NetworkInterfaces)
			md.VMMID = m.Cfg.VMID

			if setMetadata {
				serializedMetadata, err := md.AsMMDS()
				if err != nil {
					logger.Error("error while serializing metadata to mmds metadata", "reason", err)
					return err
				}
				logger.Trace("mmds enabled, setting mmds metadata", "metadata", serializedMetadata)
				m.SetMetadata(ctx, serializedMetadata)
			}

			return nil
		},
	}
}
