package build

type defers struct {
	fs []func()
}

func (d *defers) add(f func()) {
	// add in reverse order:
	d.fs = append([]func(){f}, d.fs...)
}

func (d *defers) exec() {
	for _, f := range d.fs {
		f()
	}
}
