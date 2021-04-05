package containers

import (
	"context"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/combust-labs/firebuild/pkg/build/reader"
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
		"COPY /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt",
		"ARG TARGETARCH=amd64",
		"EXPOSE 5775/udp",
		"EXPOSE 6831/udp",
		"EXPOSE 6832/udp",
		"EXPOSE 5778",
		"EXPOSE 14268",
		"EXPOSE 14250",
		"EXPOSE 16686",
		"COPY /go/bin/all-in-one-linux /go/bin/all-in-one-linux",
		"COPY /etc/jaeger/ /etc/jaeger/",
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

	tempDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal("expected temp dir, got error", err)
	}
	defer os.RemoveAll(tempDir)

	if err := ioutil.WriteFile(filepath.Join(tempDir, "Dockerfile"), []byte(strings.Join(dockerfileLines, "\n")), fs.ModePerm); err != nil {
		t.Fatal("expected Dockerfile to be written, got error", err)
	}

	readResult, err := reader.ReadFromString(filepath.Join(tempDir, "Dockerfile"), tempDir)
	if err != nil {
		t.Fatal("expected Dockerfile to be read, got error", err)
	}

	t.Log(readResult)

	exportResources := []*ImageResourceExportCommand{}
	for _, cmd := range readResult.Commands() {
		imageExportResource, err := ImageResourceExportFromCommand(cmd)
		if err != nil {
			continue
		}
		exportResources = append(exportResources, imageExportResource)
	}

	resources, err := ImageExportResources(context.Background(), dockerClient, logger, tempDir, exportResources, "jaegertracing/all-in-one:1.22")
	if err != nil {
		t.Fatal("expected resources from the Docker image, got error", err)
	}

	expectedResources := []string{
		filepath.Join(tempDir, "etc/jaeger/sampling_strategies.json"),
		filepath.Join(tempDir, "etc/ssl/certs/ca-certificates.crt"),
		filepath.Join(tempDir, "go/bin/all-in-one-linux"),
	}

	readResources := []string{}

	for _, resource := range resources {
		readResources = append(readResources, resource.ResolvedURIOrPath())
	}

	sort.Strings(expectedResources)
	sort.Strings(readResources)

	assert.Equal(t, expectedResources, readResources)

	for _, readResource := range readResources {
		_, statErr := os.Stat(readResource)
		assert.Nil(t, statErr)
	}

}
