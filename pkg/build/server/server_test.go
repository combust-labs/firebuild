package server

import (
	"fmt"
	"testing"

	"github.com/combust-labs/firebuild/pkg/build/commands"
	"github.com/combust-labs/firebuild/pkg/utilstest"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
)

type eventuallyFunc func() error

func TestServerNoContentOpsAbort(t *testing.T) {
	testWithStopType(t, func(client TestClient) {
		client.Abort(fmt.Errorf("aborted"))
	}, func(server TestServer) eventuallyFunc {
		return func() error {
			if server.Aborted() == nil {
				return fmt.Errorf("expected Aborted() to be not nil")
			}
			return nil
		}
	})
}

func TestServerNoContentOpsSuccess(t *testing.T) {
	testWithStopType(t, func(client TestClient) {
		client.Success()
	}, func(server TestServer) eventuallyFunc {
		return func() error {
			if !server.Succeeded() {
				return fmt.Errorf("expected Succeeded() to be true")
			}
			return nil
		}
	})
}

func testWithStopType(t *testing.T, stopTrigger func(TestClient), eventuallyCond func(TestServer) eventuallyFunc) {
	logger := hclog.Default()
	logger.SetLevel(hclog.Debug)

	buildCtx := &WorkContext{
		ExecutableCommands: []commands.VMInitSerializableCommand{},
		ResourcesResolved:  make(Resources),
	}

	testServer, testClient, cleanupFunc := MustStartTestGRPCServer(t, logger, buildCtx)
	defer cleanupFunc()

	expectedStderrLines := []string{"stderr line", "stderr line 2"}
	expectedStdoutLines := []string{"stdout line", "stdout line 2"}

	for _, line := range expectedStderrLines {
		testClient.StdErr([]string{line})
	}
	for _, line := range expectedStdoutLines {
		testClient.StdOut([]string{line})
	}

	stopTrigger(testClient)

	utilstest.MustEventuallyWithDefaults(t, eventuallyCond(testServer))

	assert.Equal(t, expectedStderrLines, testServer.ConsumedStderr())
	assert.Equal(t, expectedStdoutLines, testServer.ConsumedStdout())

}
