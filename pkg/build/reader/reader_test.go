package reader

import (
	"testing"

	"github.com/combust-labs/firebuild/pkg/build/commands"
)

func TestReadAddChownFromBytes(t *testing.T) {
	cmds, err := ReadFromBytes([]byte(dockerfileAddCopyChown))
	if err != nil {
		t.Fatal("Expected dockefile to parse but received an error", err)
	}
	foundAdd := false
	foundCopy := false
	for _, cmd := range cmds {
		switch tcmd := cmd.(type) {
		case commands.Add:
			foundAdd = true
			if tcmd.UserFromLocalChown == nil {
				t.Fatal("Expected ADD command with local chown")
			}
		case commands.Copy:
			foundCopy = true
			if tcmd.UserFromLocalChown == nil {
				t.Fatal("Expected COPY command with local chown")
			}
		}
	}
	if !foundAdd {
		t.Fatal("Expected ADD command")
	}
	if !foundCopy {
		t.Fatal("Expected COPY command")
	}
}

var dockerfileAddCopyChown = `FROM scracth
ADD --chown=1:2 . .
COPY --chown=1:2 . .`
