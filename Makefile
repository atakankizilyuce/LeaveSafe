BINARY_NAME=leavesafe
VERSION=1.0.0
LDFLAGS=-ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: all build build-windows build-darwin build-darwin-arm build-linux clean test test-e2e test-realtrigger fmt vet lint typos web-lint vuln check

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

# Layer 0: starts the real binary on this OS and drives the full user flow.
test-e2e:
	go test -tags e2e ./test/e2e/... -v -count=1

# Layer 2: fires the hardware changes this machine genuinely permits and prints
# a coverage matrix naming everything it could not.
test-realtrigger:
	go test -tags realtrigger ./test/realtrigger/... -v -count=1

fmt:
	go fmt ./...

vet:
	go vet ./...

# Same checks CI runs. Each tool is fetched on demand so there is nothing to
# install up front.
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...

# typos is a Rust binary; grab a release from
# https://github.com/crate-ci/typos/releases and put it on PATH.
typos:
	typos

web-lint:
	npx --yes @biomejs/biome@2.5.6 ci web/ --error-on-warnings

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

check: fmt vet lint web-lint vuln test

clean:
	rm -rf dist/ $(BINARY_NAME) $(BINARY_NAME).exe
