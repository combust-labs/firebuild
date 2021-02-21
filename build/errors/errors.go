package errors

import "fmt"

// ErrorIsDirectory is a builder directory input type string error.
type ErrorIsDirectory struct {
	Input string
}

func (e *ErrorIsDirectory) Error() string {
	return fmt.Sprintf("directory: %s", e.Input)
}

// CommandOutOfScopeError is build context error.
type CommandOutOfScopeError struct {
	Command interface{}
}

func (e *CommandOutOfScopeError) Error() string {
	return fmt.Sprintf("command out of scope: %v", e.Command)
}
