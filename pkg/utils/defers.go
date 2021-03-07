package utils

// Defers maintains ordered LIFO list of functions to handle on the defer call.
type Defers interface {
	// Add the function to the deferred list of functions.
	// The new function will be instered at the beginning of the list.
	Add(func())
	// CallAll calls all deferred functions in the reverse order.
	CallAll()
}

type defaultDefers struct {
	fs []func()
}

// NewDefers returns a new instance of Defers.
func NewDefers() Defers {
	return &defaultDefers{
		fs: []func(){},
	}
}

func (ds *defaultDefers) Add(f func()) {
	ds.fs = append([]func(){f}, ds.fs...)
}

func (ds *defaultDefers) CallAll() {
	for _, f := range ds.fs {
		f()
	}
}
