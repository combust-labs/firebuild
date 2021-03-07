package arbitrary

import (
	"github.com/firecracker-microvm/firecracker-go-sdk"
)

// HandlerWithRequirement provides a firecracker Handler with a name of the handler to append this handler after.
type HandlerWithRequirement struct {
	AppendAfter string
	Handler     firecracker.Handler
}

func NewHandlerWithRequirement(handler firecracker.Handler, requirement string) *HandlerWithRequirement {
	return &HandlerWithRequirement{
		AppendAfter: requirement,
		Handler:     handler,
	}
}

// Strategy inserts the handlers at the arbitrary required position.
type Strategy struct {
	handlerProviders []func() *HandlerWithRequirement
}

// NewStrategy returns a new ArbitraryStraetgy.
func NewStrategy(handlerProvider ...func() *HandlerWithRequirement) Strategy {
	return Strategy{
		handlerProviders: handlerProvider,
	}
}

// AddRequirements adds more arbitrary handlers with requirements.
func (s Strategy) AddRequirements(handlerProvider ...func() *HandlerWithRequirement) Strategy {
	s.handlerProviders = append(s.handlerProviders, handlerProvider...)
	return s
}

// AdaptHandlers will inject the LinkFilesHandler into the handler list.
func (s Strategy) AdaptHandlers(handlers *firecracker.Handlers) error {
	for _, handlerProvider := range s.handlerProviders {
		requirement := handlerProvider()
		if !handlers.FcInit.Has(requirement.AppendAfter) {
			return firecracker.ErrRequiredHandlerMissing
		}
		handlers.FcInit = handlers.FcInit.AppendAfter(
			requirement.AppendAfter,
			requirement.Handler,
		)
	}
	return nil
}
