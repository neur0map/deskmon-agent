.PHONY: build build-linux-amd64 build-linux-arm64 clean test run

VERSION := 0.1.0
BINARY := deskmon-agent
BUILD_DIR := bin

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY) ./cmd/deskmon-agent

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64 ./cmd/deskmon-agent

build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64 ./cmd/deskmon-agent

build-all: build-linux-amd64 build-linux-arm64

clean:
	rm -rf $(BUILD_DIR)

test:
	go test -v ./...

run:
	go run ./cmd/deskmon-agent

install:
	cp $(BUILD_DIR)/$(BINARY) /usr/local/bin/
