package build

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/build/reader"
	"github.com/combust-labs/firebuild/pkg/build/stage"
	"github.com/hashicorp/go-hclog"
)

func TestDependencyBuild(t *testing.T) {

	logger := hclog.New(&hclog.LoggerOptions{
		Level: hclog.Debug,
	})

	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal("Expected temp directory but received an error", err)
	}
	defer os.RemoveAll(tempDir)

	readResults, err := reader.ReadFromString("git+https://github.com/grepplabs/kafka-proxy.git:/Dockerfile", tempDir)
	if err != nil {
		t.Fatal("Expected dockefile to parse but received an error", err)
	}

	stages, errs := stage.ReadStages(readResults.Commands())
	if len(errs) > 0 {
		t.Log("Unexpected errors while processing stages", errs)
	}

	builderStage := stages.NamedStage("builder")
	if builderStage == nil {
		t.Fatal("Expected builder stage but found none")
	}

	// The cloned sources reside in .../sources directory, let's write our stage Dockerfile in there
	sourcesDir := filepath.Join(tempDir, "sources")

	db := NewDefaultDependencyBuild(builderStage, tempDir, sourcesDir)
	if err != nil {
		t.Fatal("Failed creating dependency builder", err)
	}

	copyCommands := []commands.Copy{
		{
			OriginalCommand: "COPY --from=builder /go/src/github.com/grepplabs/kafka-proxy/build /opt/kafka-proxy/bin",
			OriginalSource:  "",
			Stage:           "builder",
			Source:          "/go/src/github.com/grepplabs/kafka-proxy/build",
			Target:          "/opt/kafka-proxy/bin",
			Workdir:         commands.DefaultWorkdir(),
			User:            commands.DefaultUser(),
		},
	}

	resolvedResources, buildErr := db.WithLogger(logger).Build(copyCommands)
	if buildErr != nil {
		t.Fatal("Dependency build failed", buildErr)
	}

	if len(resolvedResources) == 0 {
		t.Fatal("Expected resolved resources for copy commands", copyCommands)
	}

	for _, resource := range resolvedResources {
		statInfo, statErr := os.Stat(resource.ResolvedURIOrPath())
		if statErr != nil {
			t.Error("Expected the resource for path", resource.TargetPath(), "to exist on disk but stat returned error", statErr)
			continue
		}
		t.Log("OKAY: stat for expected target", resource.TargetPath(), "==>", statInfo, "path on disk", resource.ResolvedURIOrPath())
	}

}
