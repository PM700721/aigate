BINARY := aigate
VERSION := $(shell grep 'Version =' internal/config/config.go | cut -d'"' -f2)
LDFLAGS := -s -w

.PHONY: build run clean all

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/aigate

run:
	API_KEY=test go run ./cmd/aigate

clean:
	rm -rf bin/

# Cross-compile for all platforms
all: clean
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-darwin-amd64   ./cmd/aigate
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-darwin-arm64   ./cmd/aigate
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64    ./cmd/aigate
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64    ./cmd/aigate
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-windows-amd64.exe ./cmd/aigate
