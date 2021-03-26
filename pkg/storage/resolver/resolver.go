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

// Resolver resolves the storage resolver and configures the resolved provider.
type Resolver interface {
	GetStorageImpl(logger hclog.Logger) (storage.Provider, error)
	GetStorageImplWithProvider(logger hclog.Logger, provider string) (storage.Provider, error)
	ResolveProvider(logger hclog.Logger, provider string, configProvider func() storage.FlagProvider) (storage.Provider, error)
	WithConfigurationOverride(map[string]interface{}) Resolver
	WithTypeOverride(string) Resolver
}

type defaultResolver struct {
	extraConfig  map[string]interface{}
	typeOverride string
}

// NewDefaultResolver returns an instance of the default resolver.
func NewDefaultResolver() Resolver {
	return &defaultResolver{
		extraConfig: map[string]interface{}{},
	}
}

// GetStorageImpl returns the configured resolved storage provider.
func (r *defaultResolver) GetStorageImpl(logger hclog.Logger) (storage.Provider, error) {
	provider := func() string {
		if r.typeOverride != "" {
			return r.typeOverride
		}
		return StorageProvider
	}()
	return r.ResolveProvider(logger, provider, func() storage.FlagProvider {
		switch provider {
		case "directory":
			return StorageDirectoryFlags
		default:
			return nil
		}
	})
}

// GetStorageImplWithProvider returns the configured resolved storage provider.
func (r *defaultResolver) GetStorageImplWithProvider(logger hclog.Logger, provider string) (storage.Provider, error) {
	return r.ResolveProvider(logger, provider, func() storage.FlagProvider {
		switch provider {
		case "directory":
			return StorageDirectoryFlags
		default:
			return nil
		}
	})
}

// ResolveProvider resolves the configured storage provider.
func (r *defaultResolver) ResolveProvider(logger hclog.Logger, provider string, configProvider func() storage.FlagProvider) (storage.Provider, error) {
	var impl storage.Provider
	switch provider {
	case "directory":
		impl = directory.New(logger)
	}
	if impl == nil {
		return impl, fmt.Errorf("provider %s not known", provider)
	}
	flagConfig := configProvider().GetInitializedConfiguration()
	for k, v := range r.extraConfig {
		flagConfig[k] = v
	}
	if err := impl.Configure(flagConfig); err != nil {
		return impl, errors.Wrap(err, "failed configuring provider")
	}
	return impl, nil
}

// WithConfigurationOverride adds properties to the configuration.
func (r *defaultResolver) WithConfigurationOverride(input map[string]interface{}) Resolver {
	for k, v := range input {
		r.extraConfig[k] = v
	}
	return r
}

// WithTypeOverride overrides the provider type to resolve.
func (r *defaultResolver) WithTypeOverride(input string) Resolver {
	r.typeOverride = input
	return r
}
