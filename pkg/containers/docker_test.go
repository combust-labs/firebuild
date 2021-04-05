package containers

import (
	"context"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
)

func TestDockerImagePull(t *testing.T) {

	logger := hclog.Default()
	logger.SetLevel(hclog.Debug)

	dockerClient, err := GetDefaultClient()
	assert.Nil(t, err)

	pullExpectedErr := ImagePull(context.Background(), dockerClient, logger, "alpine/3.13")
	assert.NotNil(t, pullExpectedErr)

	pullErr := ImagePull(context.Background(), dockerClient, logger, "alpine:3.13")
	assert.Nil(t, pullErr)

}

func TestDockerfileFromHistory(t *testing.T) {

	expectedLines := []string{
		"FROM alpine:3.13",
		"CMD [\"/bin/sh\"]",
		"COPY tar:///etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt",
		"ARG TARGETARCH=amd64",
		"EXPOSE 5775/udp",
		"EXPOSE 6831/udp",
		"EXPOSE 6832/udp",
		"EXPOSE 5778",
		"EXPOSE 14268",
		"EXPOSE 14250",
		"EXPOSE 16686",
		"COPY tar:///go/bin/all-in-one-linux /go/bin/all-in-one-linux",
		"COPY tar:///etc/jaeger/ /etc/jaeger/",
		"VOLUME [/tmp]",
		"ENTRYPOINT [\"/go/bin/all-in-one-linux\"]",
		"CMD [\"--sampling.strategies-file=/etc/jaeger/sampling_strategies.json\"]",
	}

	logger := hclog.Default()
	logger.SetLevel(hclog.Debug)

	dockerClient, err := GetDefaultClient()
	assert.Nil(t, err)

	pullErr := ImagePull(context.Background(), dockerClient, logger, "jaegertracing/all-in-one:1.22")
	assert.Nil(t, pullErr)

	imageMetadata, readErr := ReadImageConfig(context.Background(), dockerClient, logger, "jaegertracing/all-in-one:1.22")
	assert.Nil(t, readErr)

	dockerfileLines := HistoryToDockerfile(imageMetadata.Config.History, "alpine:3.13")

	assert.Equal(t, expectedLines, dockerfileLines)
}
