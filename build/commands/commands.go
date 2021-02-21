package commands

import (
	"fmt"
	"strings"
)

// Add represents the ADD instruction.
type Add struct {
	OriginalSource     string
	Source             string
	Target             string
	Workdir            Workdir
	User               User
	UserFromLocalChown *User
}

// Arg represents the ARG instruction.
type Arg struct {
	k, v string
	hadv bool
}

// NewRawArg returns a new parsed ARG from the raw input.
func NewRawArg(input string) (Arg, error) {
	parts := strings.Split(input, "=")
	if len(parts) == 0 {
		return Arg{}, fmt.Errorf("arg: missing name")
	}
	v, hadv := func(input []string) (string, bool) {
		if len(input) > 1 {
			return strings.Join(input[1:], "="), true
		}
		return "", false
	}(parts)
	return Arg{
		k:    parts[0],
		v:    v,
		hadv: hadv,
	}, nil
}

// Key returns the ARG key.
func (a Arg) Key() string {
	return a.k
}

// Value returns the ARG value and  a boolean indicating if value was defined in the Dockerfile.
func (a Arg) Value() (string, bool) {
	return a.v, a.hadv
}

// Cmd represents the CMD instruction.
type Cmd struct {
	Values []string
}

// Copy represents the COPY instruction.
type Copy struct {
	OriginalSource     string
	Source             string
	Stage              string
	Target             string
	Workdir            Workdir
	User               User
	UserFromLocalChown *User
}

// Entrypoint represents the ENTRYPOINT instruction.
type Entrypoint struct {
	Values  []string
	Env     map[string]string
	Shell   Shell
	Workdir Workdir
	User    User
}

// Env represents the ENV instruction.
type Env struct {
	Name  string
	Value string
}

// Expose represents the EXPOSE instruction.
type Expose struct {
	RawValue string
}

// StructuredFrom decomposes the base in=mage of From into the org, os and version parts.
type StructuredFrom struct {
	org     string
	os      string
	version string
}

// Org returns the org component of the base image.
func (sf *StructuredFrom) Org() string {
	return sf.org
}

// OS returns the OS component of the base image.
func (sf *StructuredFrom) OS() string {
	return sf.os
}

// Version returns the base image version.
func (sf *StructuredFrom) Version() string {
	return sf.version
}

// From represents the FROM instruction.
type From struct {
	BaseImage string
	StageName string
}

// ToStructuredFrom extracts structured info from the base image string.
func (f From) ToStructuredFrom() *StructuredFrom {
	structuredForm := &StructuredFrom{org: "_"}
	imageName := f.BaseImage
	if strings.Contains(f.BaseImage, "/") {
		structuredForm.org = strings.Split(f.BaseImage, "/")[0]
		imageName = strings.TrimPrefix(imageName, structuredForm.org+"/")
	}
	osAndVersion := strings.Split(imageName, ":")
	structuredForm.os = osAndVersion[0]
	structuredForm.version = osAndVersion[1]
	return structuredForm
}

// Label represents the LABEL instruction.
type Label struct {
	Key   string
	Value string
}

// Run represents the RUN instruction.
type Run struct {
	Args    map[string]string
	Command string
	Env     map[string]string
	Shell   Shell
	Workdir Workdir
	User    User
}

// Shell represents the SHELL instruction.
type Shell struct {
	Commands []string
}

// User represents the USER instruction.
type User struct {
	Value string
}

// Volume represents the VOLUME instruction.
type Volume struct {
	Workdir Workdir
	User    User
	Values  []string
}

// Workdir represents the WORKDIR instruction.
type Workdir struct {
	Value string
}

// DefaultShell returns the default shell.
func DefaultShell() Shell {
	return Shell{Commands: []string{"/bin/sh", "-c"}}
}

// DefaultUser returns the default user.
func DefaultUser() User {
	return User{Value: "0:0"}
}

// DefaultWorkdir returns the default workdir.
func DefaultWorkdir() Workdir {
	return Workdir{Value: "/"}
}

// RunWithDefaults returns a Run for a given command with defaults.
func RunWithDefaults(command string) Run {
	return Run{
		Args:    map[string]string{},
		Env:     map[string]string{},
		Command: command,
		Shell:   DefaultShell(),
		User:    DefaultUser(),
		Workdir: DefaultWorkdir(),
	}
}
