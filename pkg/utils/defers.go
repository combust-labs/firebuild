package utils

import "sync"

// Defers maintains ordered LIFO list of functions to handle on the defer call.
type Defers interface {
	// Add the function to the deferred list of functions.
	// The new function will be instered at the beginning of the list.
	Add(func())
	// CallAll calls all deferred functions in the reverse order.
	CallAll()
	// Calling Trigger(false) causes the instance not to process the defers.
	Trigger(bool)
}

type defaultDefers struct {
	sync.Mutex

	fs      []func()
	trigger bool
}

// NewDefers returns a new instance of Defers.
func NewDefers() Defers {
	return &defaultDefers{
		fs:      []func(){},
		trigger: true,
	}
}

func (ds *defaultDefers) Add(f func()) {
	ds.fs = append([]func(){f}, ds.fs...)
}

func (ds *defaultDefers) CallAll() {
	ds.Lock()
	defer ds.Unlock()
	if !ds.trigger {
		return
	}
	for _, f := range ds.fs {
		f()
	}
}

func (ds *defaultDefers) Trigger(input bool) {
	ds.Lock()
	defer ds.Unlock()
	ds.trigger = input
}
