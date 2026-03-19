BINARY_NAME=leavesafe
VERSION=1.0.0
LDFLAGS=-ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: all build build-windows build-darwin build-darwin-arm build-linux clean test fmt vet docker docker-run

all: build-windows build-darwin build-darwin-arm build-linux

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/leavesafe

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-windows-amd64.exe ./cmd/leavesafe

build-darwin:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-amd64 ./cmd/leavesafe

build-darwin-arm:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-arm64 ./cmd/leavesafe

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-amd64 ./cmd/leavesafe

test:
	go test ./... -v

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf dist/ $(BINARY_NAME) $(BINARY_NAME).exe

docker:
	docker build -t $(BINARY_NAME) .

docker-run:
	docker run --rm -it -p 8080:8080 -e PORT=8080 -e CONTAINER=1 $(BINARY_NAME)
