package resolver

import (
	"fmt"

	"github.com/combust-labs/firebuild/pkg/storage"
	"github.com/combust-labs/firebuild/pkg/storage/directory"
	"github.com/hashicorp/go-hclog"
	"github.com/pkg/errors"
)

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
