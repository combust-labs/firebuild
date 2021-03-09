package naming

import (
	"github.com/combust-labs/firebuild/pkg/utils"
)

const (
	// MetadataFileName is the name of the file in which the accompanying rootfs metadata is stored.
	MetadataFileName = "metadata.json"
	// RootfsEnvVarsFile is the location of the env variables
	// extracted from the Docker build.
	RootfsEnvVarsFile = "/etc/profile.d/rootfs-env.sh"
	// RootfsFileName is the base name of the root file system, as stored on disk.
	RootfsFileName = "rootfs"
	// RunEnvVarsFile is the location of the env variables
	// extracted from the Docker build.
	RunEnvVarsFile = "/etc/profile.d/run-env.sh"

	// ServiceInstallerFile is the installer file deployed during the rootfs build,
	// when --service-file-installer is defined.
	// TODO: remove this setting and the functionality, implement a method to pass a command to start the program when the VMM boots.
	ServiceInstallerFile = "/etc/firebuild/installer.sh"
)

// GetRandomVethName returns a random veth interface name.
func GetRandomVethName() string {
	return "veth" + utils.RandStringBytes(11)
}
