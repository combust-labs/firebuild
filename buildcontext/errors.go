package buildcontext

import "fmt"

// ErrorIsDirectory is a builder directory input type string error.
type ErrorIsDirectory struct {
	Input string
}

func (e *ErrorIsDirectory) Error() string {
	return fmt.Sprintf("directory: %s", e.Input)
}
