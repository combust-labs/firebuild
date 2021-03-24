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
	OriginalCommand    string  `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	OriginalSource     string  `json:"OriginalSource" mapstructure:"OriginalSource"`
	Source             string  `json:"Source" mapstructure:"Source"`
	Target             string  `json:"Target" mapstructure:"Target"`
	Workdir            Workdir `json:"Workdir" mapstructure:"Workdir"`
	User               User    `json:"User" mapstructure:"User"`
	UserFromLocalChown *User   `json:"UserFromLocalChown" mapstructure:"UserFromLocalChown"`
}

func (cmd Add) GetOriginal() string {
	return cmd.OriginalCommand
}

// Arg represents the ARG instruction.
type Arg struct {
	OriginalCommand string `json:"OriginalCommand" mapstructure:"OriginalCommand"`
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
	OriginalCommand string   `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	Values          []string `json:"values" mapstructure:"values"`
}

func (cmd Cmd) GetOriginal() string {
	return cmd.OriginalCommand
}

// Copy represents the COPY instruction.
type Copy struct {
	OriginalCommand    string  `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	OriginalSource     string  `json:"OriginalSource" mapstructure:"OriginalSource"`
	Source             string  `json:"Source" mapstructure:"Source"`
	Stage              string  `json:"Stage" mapstructure:"Stage"`
	Target             string  `json:"Target" mapstructure:"Target"`
	Workdir            Workdir `json:"Workdir" mapstructure:"Workdir"`
	User               User    `json:"User" mapstructure:"User"`
	UserFromLocalChown *User   `json:"UserFromLocalChown" mapstructure:"UserFromLocalChown"`
}

func (cmd Copy) GetOriginal() string {
	return cmd.OriginalCommand
}

// Entrypoint represents the ENTRYPOINT instruction.
type Entrypoint struct {
	OriginalCommand string            `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	Values          []string          `json:"Values" mapstructure:"Values"`
	Env             map[string]string `json:"Env" mapstructure:"Env"`
	Shell           Shell             `json:"Shell" mapstructure:"Shell"`
	Workdir         Workdir           `json:"Workdir" mapstructure:"Workdir"`
	User            User              `json:"User" mapstructure:"User"`
}

func (cmd Entrypoint) GetOriginal() string {
	return cmd.OriginalCommand
}

// Env represents the ENV instruction.
type Env struct {
	OriginalCommand string `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	Name            string `json:"Name" mapstructure:"Name"`
	Value           string `json:"Value" mapstructure:"Value"`
}

func (cmd Env) GetOriginal() string {
	return cmd.OriginalCommand
}

// Expose represents the EXPOSE instruction.
type Expose struct {
	OriginalCommand string `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	RawValue        string `json:"RawValue" mapstructure:"RawValue"`
}

func (cmd Expose) GetOriginal() string {
	return cmd.OriginalCommand
}

// StructuredFrom decomposes the base in=mage of From into the org, os and version parts.
type StructuredFrom struct {
	org     string
	image   string
	version string
}

// Org returns the org component of the base image.
func (sf *StructuredFrom) Org() string {
	return sf.org
}

// Image returns the image component of the base image.
func (sf *StructuredFrom) Image() string {
	return sf.image
}

// Version returns the base image version.
func (sf *StructuredFrom) Version() string {
	return sf.version
}

// From represents the FROM instruction.
type From struct {
	OriginalCommand string `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	BaseImage       string `json:"BaseImage" mapstructure:"BaseImage"`
	StageName       string `json:"StageName" mapstructure:"StageName"`
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
	structuredForm.image = osAndVersion[0]
	structuredForm.version = osAndVersion[1]
	return structuredForm
}

// Label represents the LABEL instruction.
type Label struct {
	OriginalCommand string `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	Key             string `json:"Key" mapstructure:"Key"`
	Value           string `json:"Value" mapstructure:"Value"`
}

func (cmd Label) GetOriginal() string {
	return cmd.OriginalCommand
}

// Run represents the RUN instruction.
type Run struct {
	OriginalCommand string            `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	Args            map[string]string `json:"Args" mapstructure:"Args"`
	Command         string            `json:"Command" mapstructure:"Command"`
	Env             map[string]string `json:"Env" mapstructure:"Env"`
	Shell           Shell             `json:"Shell" mapstructure:"Shell"`
	Workdir         Workdir           `json:"Workdir" mapstructure:"Workdir"`
	User            User              `json:"User" mapstructure:"User"`
}

func (cmd Run) GetOriginal() string {
	return cmd.OriginalCommand
}

// Shell represents the SHELL instruction.
type Shell struct {
	OriginalCommand string   `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	Commands        []string `json:"Commands" mapstructure:"Commands"`
}

func (cmd Shell) GetOriginal() string {
	return cmd.OriginalCommand
}

// User represents the USER instruction.
type User struct {
	OriginalCommand string `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	Value           string `json:"Value" mapstructure:"Value"`
}

func (cmd User) GetOriginal() string {
	return cmd.OriginalCommand
}

// Volume represents the VOLUME instruction.
type Volume struct {
	OriginalCommand string   `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	Workdir         Workdir  `json:"Workdir" mapstructure:"Workdir"`
	User            User     `json:"User" mapstructure:"User"`
	Values          []string `json:"Values" mapstructure:"Values"`
}

// Workdir represents the WORKDIR instruction.
type Workdir struct {
	OriginalCommand string `json:"OriginalCommand" mapstructure:"OriginalCommand"`
	Value           string `json:"Value" mapstructure:"Value"`
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
