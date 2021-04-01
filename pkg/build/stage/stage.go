package stage

import (
	"github.com/combust-labs/firebuild-shared/build/commands"
	bcErrors "github.com/combust-labs/firebuild/pkg/build/errors"
)

// ReadStages reads the stages out of the source commands.
func ReadStages(inputs []interface{}) (Stages, []error) {
	stages := newStages()
	errs := []error{}

	danglingCommands := []interface{}{}

	for _, input := range inputs {
		switch input.(type) {
		case commands.From:
			// a FROM command resets the processing stage
			stages.closePrevious()
			stages.setCurrent(newEmptyStage())
			for _, danglingCommand := range danglingCommands {
				stages.addCommand(danglingCommand)
			}
			stages.addCommand(input)
		default:
			if !stages.addCommand(input) {
				// if there is an ARG, ENV or LABEL prior to any FROM,
				// remember it and add these to any further stage first
				switch input.(type) {
				case commands.Arg:
					danglingCommands = append(danglingCommands, input)
				case commands.Env:
					danglingCommands = append(danglingCommands, input)
				case commands.Label:
					danglingCommands = append(danglingCommands, input)
				default:
					errs = append(errs, &bcErrors.CommandOutOfScopeError{Command: input})
				}
			}
		}
	}
	stages.closePrevious()
	return stages, errs
}

// Stages represents all build stages parsed out of the Dockerfile.
// Items are ordered accordingly to how they are defnied in the Dockerfile.
type Stages interface {
	addCommand(interface{}) bool
	setCurrent(Stage)
	closePrevious()
	// Public interface:
	All() []Stage
	NamedStage(string) Stage
	Named() []Stage
	Unnamed() []Stage
}

type stages struct {
	current Stage
	stages  []Stage
}

func newStages() Stages {
	return &stages{
		stages: []Stage{},
	}
}

func (ps *stages) addCommand(command interface{}) bool {
	if ps.current != nil {
		ps.current.addCommand(command)
		return true
	}
	return false
}

func (ps *stages) closePrevious() {
	if ps.current != nil {
		ps.stages = append(ps.stages, ps.current)
		ps.current = nil
	}
}

func (ps *stages) setCurrent(s Stage) {
	ps.current = s
}

func (ps *stages) All() []Stage {
	return ps.stages
}

func (ps *stages) NamedStage(scopeName string) Stage {
	for _, s := range ps.stages {
		if s.Name() == scopeName {
			return s
		}
	}
	return nil
}

func (ps *stages) Named() (scs []Stage) {
	for _, s := range ps.stages {
		if s.IsNamed() {
			scs = append(scs, s)
		}
	}
	return scs
}

func (ps *stages) Unnamed() (scs []Stage) {
	for _, s := range ps.stages {
		if !s.IsNamed() {
			scs = append(scs, s)
		}
	}
	return scs
}

// Stage represents a single FROM with dependent commands.
type Stage interface {
	addCommand(interface{})
	// Public interface:
	Commands() []interface{}
	DependsOn() []string
	IsNamed() bool
	IsValid() bool
	Name() string
}

type stage struct {
	commands     []interface{}
	dependsOn    map[string]bool
	hasValidFrom bool
	name         string
}

func newEmptyStage() Stage {
	return &stage{
		commands:  []interface{}{},
		dependsOn: map[string]bool{},
	}
}

func (ps *stage) addCommand(cmd interface{}) {
	switch tcmd := cmd.(type) {
	case commands.From:
		ps.name = tcmd.StageName
		ps.hasValidFrom = tcmd.BaseImage != ""
	case commands.Copy:
		if tcmd.Stage != "" {
			ps.dependsOn[tcmd.Stage] = true
		}
	}
	ps.commands = append(ps.commands, cmd)
}

func (ps *stage) Commands() []interface{} {
	return ps.commands
}

func (ps *stage) DependsOn() []string {
	return func() []string {
		keys := []string{}
		for k := range ps.dependsOn {
			keys = append(keys, k)
		}
		return keys
	}()
}

func (ps *stage) IsNamed() bool {
	return ps.name != ""
}
func (ps *stage) IsValid() bool {
	return ps.hasValidFrom
}

func (ps *stage) Name() string {
	return ps.name
}
