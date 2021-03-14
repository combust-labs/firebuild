package flags

import (
	"github.com/combust-labs/firebuild/pkg/storage"
	"github.com/spf13/pflag"
)

// FlagProvider is the Keto provider.
type flags struct {
	KernelStorageRoot string
	RootfsStorageRoot string
}

// New returns an initialized instance of the flag provider.
func New() storage.FlagProvider {
	return &flags{}
}

func (fp *flags) GetFlags() *pflag.FlagSet {
	set := &pflag.FlagSet{}
	set.StringVar(&fp.KernelStorageRoot, "storage-provider.directory.kernel-storage-root", "", "Full path to the root directory of the kernel storage")
	set.StringVar(&fp.RootfsStorageRoot, "storage-provider.directory.rootfs-storage-root", "", "Full path to the root directory of the rootfs storage")
	return set
}

func (fp *flags) GetInitializedConfiguration() map[string]interface{} {
	return map[string]interface{}{
		"kernel-storage-root": fp.KernelStorageRoot,
		"rootfs-storage-root": fp.RootfsStorageRoot,
	}
}
