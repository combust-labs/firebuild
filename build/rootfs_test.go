package build

import (
	"fmt"
	"testing"

	"github.com/docker/docker/pkg/fileutils"
)

func TestDockerignoreMatches(t *testing.T) {
	patternMatcher, err := fileutils.NewPatternMatcher([]string{
		".DS_Store",
		"**/.DS_Store",
		"vendor/",
		"**/vendor",
		"!internal/vendor",
		"data/**",
		"!data/must-exist",
	})
	if err != nil {
		t.Fatal("Expected pattern matcher to be created")
	}

	tests := map[string]bool{
		".DS_Store":            true,
		"aaa/bbb/.DS_Store":    true,
		"aaa/bbb/.DS_Storeeee": false,
		"vendor/aaa/bbb/ccc":   true,
		"vendor":               true,
		"someother/vendor":     true,
		"internal/vendor":      false,
		"data/aaa/bbb":         true,
		"data/must-exist":      false,
	}

	for k, v := range tests {
		status, _ := patternMatcher.Matches(k)
		if status != v {
			t.Error("Expected != result for path", k, fmt.Sprintf("%v vs %v", v, status))
		}
	}
}
