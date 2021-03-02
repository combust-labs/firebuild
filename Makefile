PHONY: test-dependency-build
test-dependency-build:
	/usr/local/go/bin/go test -timeout 120s -tags sqlite -run ^TestDependencyBuild$ github.com/appministry/firebuild/build -v -count=1