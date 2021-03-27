package build

import (
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/build/reader"
	"github.com/combust-labs/firebuild/pkg/build/resources"
	"github.com/combust-labs/firebuild/pkg/build/server"
	"github.com/combust-labs/firebuild/pkg/utilstest"
	"github.com/docker/docker/pkg/fileutils"
	"github.com/hashicorp/go-hclog"

	"github.com/stretchr/testify/assert"
)

func mustNewArg(t *testing.T, rawValue string) commands.Arg {
	arg, err := commands.NewRawArg(rawValue)
	if err != nil {
		t.Fatal(err)
	}
	return arg
}

func TestContextBuilderSingleStageWithResources(t *testing.T) {

	logger := hclog.Default()
	logger.SetLevel(hclog.Debug)

	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal("expected temp dir, got error", err)
	}
	defer os.RemoveAll(tempDir)

	expectedResource1Bytes := []byte("resource 1 content")
	expectedResource2Bytes := []byte("resource 2 content")

	if err := ioutil.WriteFile(filepath.Join(tempDir, "Dockerfile"), []byte(testDockerfile1), fs.ModePerm); err != nil {
		t.Fatal("expected Dockerfile to be written, got error", err)
	}
	if err := ioutil.WriteFile(filepath.Join(tempDir, "resource1"), expectedResource1Bytes, fs.ModePerm); err != nil {
		t.Fatal("expected Dockerfile to be written, got error", err)
	}
	if err := ioutil.WriteFile(filepath.Join(tempDir, "resource2"), expectedResource2Bytes, fs.ModePerm); err != nil {
		t.Fatal("expected Dockerfile to be written, got error", err)
	}
	readResult, err := reader.ReadFromString(filepath.Join(tempDir, "Dockerfile"), tempDir)
	if err != nil {
		t.Fatal("expected Dockerfile to be read, got error", err)
	}

	contextBuilder := NewDefaultBuild()
	if err := contextBuilder.AddInstructions(readResult.Commands()...); err != nil {
		t.Fatal("expected commands to be added, got error", err)
	}
	buildCtx, err := contextBuilder.WithResolver(resources.NewDefaultResolver()).CreateContext(make(server.Resources))
	if err != nil {
		t.Fatal("expected build context to be created, got error", err)
	}

	grpcConfig := &server.GRPCServiceConfig{
		ServerName:        "test-grpc-server",
		BindHostPort:      "127.0.0.1:0",
		EmbeddedCAKeySize: 1024, // use this low for tests only! low value speeds up tests
	}

	testServer := server.NewTestServer(t, logger.Named("grpc-server"), grpcConfig, buildCtx)
	testServer.Start()

	select {
	case startErr := <-testServer.FailedNotify():
		t.Fatal("expected the GRPC server to start but it failed", startErr)
	case <-testServer.ReadyNotify():
		t.Log("GRPC server started and serving on", grpcConfig.BindHostPort)
		defer testServer.Stop()
	}

	testClient, clientErr := server.NewTestClient(t, logger.Named("grpc-client"), grpcConfig)
	if clientErr != nil {
		t.Fatal("expected the GRPC client, got error", clientErr)
	}

	opErr := testClient.Commands(t)
	if opErr != nil {
		t.Fatal("GRPC client Commands() opErr", opErr)
	}

	nextCommand := testClient.NextCommand()
	if _, ok := nextCommand.(commands.Run); !ok {
		t.Fatal("expected RUN command")
	}

	nextCommand = testClient.NextCommand()
	if addCommand, ok := nextCommand.(commands.Add); !ok {
		t.Fatal("expected ADD command")
	} else {
		resourceChannel, err := testClient.Resource(addCommand.Source)
		if err != nil {
			t.Fatal("expected resource channel for ADD command, got error", err)
		}
		resource := <-resourceChannel
		resourceData, err := mustReadFromReader(resource.Contents())
		if err != nil {
			t.Fatal("expected resource to read, got error", err)
		}
		assert.Equal(t, expectedResource1Bytes, resourceData)
	}

	nextCommand = testClient.NextCommand()
	if copyCommand, ok := nextCommand.(commands.Copy); !ok {
		t.Fatal("expected COPY command")
	} else {
		resourceChannel, err := testClient.Resource(copyCommand.Source)
		if err != nil {
			t.Fatal("expected resource channel for ADD command, got error", err)
		}
		resource := <-resourceChannel
		resourceData, err := mustReadFromReader(resource.Contents())
		if err != nil {
			t.Fatal("expected resource to read, got error", err)
		}
		assert.Equal(t, expectedResource2Bytes, resourceData)
	}

	nextCommand = testClient.NextCommand()
	if _, ok := nextCommand.(commands.Run); !ok {
		t.Fatal("expected RUN command")
	}

	assert.Nil(t, testClient.NextCommand())

	expectedStderrLines := []string{"stderr line", "stderr line 2"}
	expectedStdoutLines := []string{"stdout line", "stdout line 2"}

	for _, line := range expectedStderrLines {
		testClient.StdErr([]string{line})
	}
	for _, line := range expectedStdoutLines {
		testClient.StdOut([]string{line})
	}

	testClient.Abort(fmt.Errorf("client aborted"))

	<-testServer.FinishedNotify()

	utilstest.MustEventuallyWithDefaults(t, func() error {
		if testServer.Aborted() == nil {
			return fmt.Errorf("expected Aborted() to be not nil")
		}
		return nil
	})

	assert.Equal(t, expectedStderrLines, testServer.ConsumedStderr())
	assert.Equal(t, expectedStdoutLines, testServer.ConsumedStdout())

}

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

func mustReadFromReader(reader io.ReadCloser, _ error) ([]byte, error) {
	return ioutil.ReadAll(reader)
}

const testDockerfile1 = `FROM alpine:3.13
ARG PARAM1=value
ENV ENVPARAM1=envparam1
RUN mkdir -p /dir
ADD resource1 /target/resource1
COPY resource2 /target/resource2
RUN cp /dir/${ENVPARAM1} \
	&& call --arg=${PARAM1}`
