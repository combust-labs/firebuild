package arbitrary

import (
	"github.com/firecracker-microvm/firecracker-go-sdk"
)

// HandlerPlacement provides a firecracker handler placement instruction.
// The handler will be placed after the requirement.
type HandlerPlacement struct {
	AppendAfter string
	Handler     firecracker.Handler
}

// NewHandlerPlacement creates a new handler placement.
func NewHandlerPlacement(handler firecracker.Handler, requirement string) *HandlerPlacement {
	return &HandlerPlacement{
		AppendAfter: requirement,
		Handler:     handler,
	}
}

// PlacingStrategy inserts the handlers at the arbitrary required position.
type PlacingStrategy struct {
	handlerPlacements []func() *HandlerPlacement
}

// NewStrategy returns a new PlacingStrategy.
func NewStrategy(handlerPlacement ...func() *HandlerPlacement) PlacingStrategy {
	return PlacingStrategy{
		handlerPlacements: handlerPlacement,
	}
}

// AddRequirements adds more arbitrary handlers with requirements.
func (s PlacingStrategy) AddRequirements(handlerPlacement ...func() *HandlerPlacement) PlacingStrategy {
	s.handlerPlacements = append(s.handlerPlacements, handlerPlacement...)
	return s
}

// AdaptHandlers will inject the LinkFilesHandler into the handler list.
func (s PlacingStrategy) AdaptHandlers(handlers *firecracker.Handlers) error {
	for _, placementDef := range s.handlerPlacements {
		placement := placementDef()
		if !handlers.FcInit.Has(placement.AppendAfter) {
			return firecracker.ErrRequiredHandlerMissing
		}
		handlers.FcInit = handlers.FcInit.AppendAfter(
			placement.AppendAfter,
			placement.Handler,
		)
	}
	return nil
}
