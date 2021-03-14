package resolver

import (
	"fmt"

	"github.com/combust-labs/firebuild/pkg/storage"
	"github.com/combust-labs/firebuild/pkg/storage/directory"
	"github.com/hashicorp/go-hclog"
	"github.com/pkg/errors"

	directoryFlags "github.com/combust-labs/firebuild/pkg/storage/directory/flags"
	"github.com/spf13/pflag"
)

var (
	// StorageProvider is the configured storage provider.
	StorageProvider = ""
	// StorageDirectoryFlags provides the flags for the directory storage.
	StorageDirectoryFlags = directoryFlags.New()
)

// AddStorageFlags sets up storage provider flags.
func AddStorageFlags(set *pflag.FlagSet) {
	set.StringVar(&StorageProvider, "storage-provider", "", "Storage provider to use")
	set.AddFlagSet(StorageDirectoryFlags.GetFlags())
}

// GetStorageImpl returns the configured resolved storage provider.
func GetStorageImpl(logger hclog.Logger) (storage.Provider, error) {
	return ResolveProvider(logger, StorageProvider, func() storage.FlagProvider {
		switch StorageProvider {
		case "directory":
			return StorageDirectoryFlags
		default:
			return nil
		}
	})
}

// GetStorageImplWithProvider returns the configured resolved storage provider.
func GetStorageImplWithProvider(logger hclog.Logger, provider string) (storage.Provider, error) {
	return ResolveProvider(logger, provider, func() storage.FlagProvider {
		switch provider {
		case "directory":
			return StorageDirectoryFlags
		default:
			return nil
		}
	})
}

// ResolveProvider resolves the configured storage provider.
func ResolveProvider(logger hclog.Logger, provider string, configProvider func() storage.FlagProvider) (storage.Provider, error) {
	var impl storage.Provider
	switch provider {
	case "directory":
		impl = directory.New(logger)
	}
	if impl == nil {
		return impl, fmt.Errorf("provider %s not known", provider)
	}
	if err := impl.Configure(configProvider().GetInitializedConfiguration()); err != nil {
		return impl, errors.Wrap(err, "failed configuring provider")
	}
	return impl, nil
}
