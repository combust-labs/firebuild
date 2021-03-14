package configs

import (
	"sync"

	profileModel "github.com/combust-labs/firebuild/pkg/profiles/model"
	"github.com/spf13/pflag"
)

type flagBase struct {
	sync.Mutex
	flagSet *pflag.FlagSet
}

func (fb *flagBase) initFlagSet() bool {
	fb.Lock()
	defer fb.Unlock()
	if fb.flagSet == nil {
		fb.flagSet = &pflag.FlagSet{}
		return true
	}
	return false
}

// ValidatingConfig is a config which can be validated.
type ValidatingConfig interface {
	Validate() error
}

// ProfileInheriting is a config which can take a profile configuration and amend its state
// from the profile provided settings.
type ProfileInheriting interface {
	UpdateFromProfile(*profileModel.Profile) error
}
