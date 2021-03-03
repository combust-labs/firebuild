package stage

import (
	"sort"
	"testing"

	"github.com/combust-labs/firebuild/build/reader"
)

func TestMultipleStages(t *testing.T) {
	commands, err := reader.ReadFromBytes([]byte(dockerfileKafkaProxy))
	if err != nil {
		t.Fatal("Expected dockefile to parse but received an error", err)
	}
	scs, errs := ReadStages(commands)
	if len(errs) > 0 {
		t.Fatal("Stages reader returned errors", errs)
	}
	pss := scs.All()
	if len(pss) != 2 {
		t.Fatal("Expected 2 processable stages")
	}

	named := scs.NamedStage("builder")
	if named == nil {
		t.Fatal("Expected builder stage to exist in parsed stages")
	}
	if len(named.DependsOn()) > 0 {
		t.Fatal("Expected named scope to not depend on other stages")
	}

	if scs.NamedStage("non-existing") != nil {
		t.Fatal("Expected non-existing stage to not exist in parsed stages")
	}

	unnamed := scs.Unnamed()
	if len(unnamed) != 1 {
		t.Fatal("Expected exactly 1 unnamed scope")
	}
	mainScope := unnamed[0]
	expectedDependsOn := []string{"builder"}
	if !stringArraysTheSame(mainScope.DependsOn(), expectedDependsOn) {
		t.Fatalf("Expected %+v depend on but received %+v", expectedDependsOn, mainScope.DependsOn())
	}
}

func stringArraysTheSame(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var dockerfileKafkaProxy = `FROM golang:1.15-alpine3.12 as builder
RUN apk add alpine-sdk ca-certificates

WORKDIR /go/src/github.com/grepplabs/kafka-proxy
COPY . .

ARG MAKE_TARGET=build
ARG GOOS=linux
ARG GOARCH=amd64
RUN make -e GOARCH=${GOARCH} -e GOOS=${GOOS} clean ${MAKE_TARGET}

FROM alpine:3.12
RUN apk add --no-cache ca-certificates

COPY --from=builder /go/src/github.com/grepplabs/kafka-proxy/build /opt/kafka-proxy/bin
ENTRYPOINT ["/opt/kafka-proxy/bin/kafka-proxy"]
CMD ["--help"]
`
