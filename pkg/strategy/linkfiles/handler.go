package linkfiles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/firecracker-microvm/firecracker-go-sdk"
)

const LinkFilesToRootFSHandlerName = "fcinit.LinkFilesToRootFS"
const rootfsFolderName = "root"

// LinkFilesHandler creates a new link files handler that will link files to
// the rootfs
func LinkFilesHandler(kernelImageFileName string) firecracker.Handler {
	return firecracker.Handler{
		Name: LinkFilesToRootFSHandlerName,
		Fn: func(ctx context.Context, m *firecracker.Machine) error {

			fmt.Println(" ===============> am I even here?")

			if m.Cfg.JailerCfg == nil {
				return firecracker.ErrMissingJailerConfig
			}

			// assemble the path to the jailed root folder on the host
			rootfs := filepath.Join(
				m.Cfg.JailerCfg.ChrootBaseDir,
				filepath.Base(m.Cfg.JailerCfg.ExecFile),
				m.Cfg.JailerCfg.ID,
				rootfsFolderName,
			)

			// copy kernel image to root fs
			if err := os.Link(
				m.Cfg.KernelImagePath,
				filepath.Join(rootfs, kernelImageFileName),
			); err != nil {
				return err
			}

			initrdFilename := ""
			if m.Cfg.InitrdPath != "" {
				initrdFilename := filepath.Base(m.Cfg.InitrdPath)
				// copy initrd to root fs
				if err := os.Link(
					m.Cfg.InitrdPath,
					filepath.Join(rootfs, initrdFilename),
				); err != nil {
					return err
				}
			}

			// copy all drives to the root fs
			for i, drive := range m.Cfg.Drives {
				hostPath := firecracker.StringValue(drive.PathOnHost)
				driveFileName := filepath.Base(hostPath)

				if err := os.Link(
					hostPath,
					filepath.Join(rootfs, driveFileName),
				); err != nil {
					return err
				}

				m.Cfg.Drives[i].PathOnHost = firecracker.String(driveFileName)
			}

			m.Cfg.KernelImagePath = kernelImageFileName
			if m.Cfg.InitrdPath != "" {
				m.Cfg.InitrdPath = initrdFilename
			}

			for _, fifoPath := range []*string{&m.Cfg.LogFifo, &m.Cfg.MetricsFifo} {
				if fifoPath == nil || *fifoPath == "" {
					continue
				}

				fileName := filepath.Base(*fifoPath)
				if err := os.Link(
					*fifoPath,
					filepath.Join(rootfs, fileName),
				); err != nil {
					return err
				}

				if err := os.Chown(filepath.Join(rootfs, fileName), *m.Cfg.JailerCfg.UID, *m.Cfg.JailerCfg.GID); err != nil {
					return err
				}

				// update fifoPath as jailer works relative to the chroot dir
				*fifoPath = fileName
			}

			return nil
		},
	}
}
