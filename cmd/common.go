package cmd

import (
	"github.com/combust-labs/firebuild/pkg/storage"
	directoryFlags "github.com/combust-labs/firebuild/pkg/storage/directory/flags"
	"github.com/combust-labs/firebuild/pkg/storage/resolver"
	"github.com/hashicorp/go-hclog"
	"github.com/spf13/pflag"
)

// RootFSCopyBufferSize is the buffer size for root file system copy operation.
const RootFSCopyBufferSize = 4 * 1024 * 1024

var (
	// StorageProvider is the configured storage provider.
	StorageProvider = ""
	// StorageDirectoryFlags provides the flags for the directory storage.
	StorageDirectoryFlags = directoryFlags.New()
)

// AddStorageFlags sets up storage provider flags.
func AddStorageFlags(set *pflag.FlagSet) {
	set.StringVar(&StorageProvider, "storage.provider", "", "Storage provider to use")
	set.AddFlagSet(StorageDirectoryFlags.GetFlags())
}

// GetStorageImpl returns the configured resolved storage provider.
func GetStorageImpl(logger hclog.Logger) (storage.Provider, error) {
	return resolver.ResolveProvider(logger.With(), StorageProvider, func() storage.FlagProvider {
		switch StorageProvider {
		case "directory":
			return StorageDirectoryFlags
		default:
			return nil
		}
	})
}
