package commands

import (
	"fmt"
	"strings"
)

type DockerfileSerializable interface {
	GetOriginal() string
}

// Add represents the ADD instruction.
type Add struct {
	OriginalCommand    string
	OriginalSource     string
	Source             string
	Target             string
	Workdir            Workdir
	User               User
	UserFromLocalChown *User
}

func (cmd Add) GetOriginal() string {
	return cmd.OriginalCommand
}

// Arg represents the ARG instruction.
type Arg struct {
	OriginalCommand string
	k, v            string
	hadv            bool
}

func (cmd Arg) GetOriginal() string {
	return cmd.OriginalCommand
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
func (cmd Arg) Key() string {
	return cmd.k
}

// Value returns the ARG value and  a boolean indicating if value was defined in the Dockerfile.
func (cmd Arg) Value() (string, bool) {
	return cmd.v, cmd.hadv
}

// Cmd represents the CMD instruction.
type Cmd struct {
	OriginalCommand string
	Values          []string
}

func (cmd Cmd) GetOriginal() string {
	return cmd.OriginalCommand
}

// Copy represents the COPY instruction.
type Copy struct {
	OriginalCommand    string
	OriginalSource     string
	Source             string
	Stage              string
	Target             string
	Workdir            Workdir
	User               User
	UserFromLocalChown *User
}

func (cmd Copy) GetOriginal() string {
	return cmd.OriginalCommand
}

// Entrypoint represents the ENTRYPOINT instruction.
type Entrypoint struct {
	OriginalCommand string
	Values          []string
	Env             map[string]string
	Shell           Shell
	Workdir         Workdir
	User            User
}

func (cmd Entrypoint) GetOriginal() string {
	return cmd.OriginalCommand
}

// Env represents the ENV instruction.
type Env struct {
	OriginalCommand string
	Name            string
	Value           string
}

func (cmd Env) GetOriginal() string {
	return cmd.OriginalCommand
}

// Expose represents the EXPOSE instruction.
type Expose struct {
	OriginalCommand string
	RawValue        string
}

func (cmd Expose) GetOriginal() string {
	return cmd.OriginalCommand
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
	OriginalCommand string
	BaseImage       string
	StageName       string
}

func (cmd From) GetOriginal() string {
	return cmd.OriginalCommand
}

// ToStructuredFrom extracts structured info from the base image string.
func (cmd From) ToStructuredFrom() *StructuredFrom {
	structuredForm := &StructuredFrom{org: "_"}
	imageName := cmd.BaseImage
	if strings.Contains(cmd.BaseImage, "/") {
		structuredForm.org = strings.Split(cmd.BaseImage, "/")[0]
		imageName = strings.TrimPrefix(imageName, structuredForm.org+"/")
	}
	osAndVersion := strings.Split(imageName, ":")
	structuredForm.os = osAndVersion[0]
	structuredForm.version = osAndVersion[1]
	return structuredForm
}

// Label represents the LABEL instruction.
type Label struct {
	OriginalCommand string
	Key             string
	Value           string
}

func (cmd Label) GetOriginal() string {
	return cmd.OriginalCommand
}

// Run represents the RUN instruction.
type Run struct {
	OriginalCommand string
	Args            map[string]string
	Command         string
	Env             map[string]string
	Shell           Shell
	Workdir         Workdir
	User            User
}

func (cmd Run) GetOriginal() string {
	return cmd.OriginalCommand
}

// Shell represents the SHELL instruction.
type Shell struct {
	OriginalCommand string
	Commands        []string
}

func (cmd Shell) GetOriginal() string {
	return cmd.OriginalCommand
}

// User represents the USER instruction.
type User struct {
	OriginalCommand string
	Value           string
}

func (cmd User) GetOriginal() string {
	return cmd.OriginalCommand
}

// Volume represents the VOLUME instruction.
type Volume struct {
	OriginalCommand string
	Workdir         Workdir
	User            User
	Values          []string
}

// Workdir represents the WORKDIR instruction.
type Workdir struct {
	OriginalCommand string
	Value           string
}

func (cmd Workdir) GetOriginal() string {
	return cmd.OriginalCommand
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
