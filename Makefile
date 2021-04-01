PHONY: test-dependency-build

.PHONY: lint
lint:
	golint ./...

test-dependency-build:
	/usr/local/go/bin/go test -timeout 120s -tags sqlite -run ^TestDependencyBuild$ github.com/combust-labs/firebuild/build -v -count=1