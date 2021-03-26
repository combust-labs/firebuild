PHONY: test-dependency-build

.PHONY: lint
lint:
	golint ./...

.PHONY: genproto
genproto:
	protoc -I=./grpc/proto \
		--go_out=./grpc/proto \
			--go_opt=paths=source_relative \
		--go-grpc_out=./grpc/proto \
			--go-grpc_opt=paths=source_relative \
			--go-grpc_opt=require_unimplemented_servers=false \
		./grpc/proto/rootfs_server.proto

.PHONY: prototools
prototools:
	go get -u github.com/golang/protobuf/proto \
		github.com/golang/protobuf/protoc-gen-go \
		google.golang.org/grpc \
		google.golang.org/protobuf/cmd/protoc-gen-go \
		google.golang.org/grpc/cmd/protoc-gen-go-grpc
	go install google.golang.org/protobuf/cmd/protoc-gen-go \
		google.golang.org/grpc/cmd/protoc-gen-go-grpc
	go mod tidy

test-dependency-build:
	/usr/local/go/bin/go test -timeout 120s -tags sqlite -run ^TestDependencyBuild$ github.com/combust-labs/firebuild/build -v -count=1