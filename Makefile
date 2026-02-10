.PHONY: build build-linux-amd64 build-linux-arm64 build-all clean test run \
       package-amd64 package-arm64 package-all

VERSION := 0.1.0
BINARY := deskmon-agent
BUILD_DIR := bin
DIST_DIR := dist

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY) ./cmd/deskmon-agent

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64 ./cmd/deskmon-agent

build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64 ./cmd/deskmon-agent

build-all: build-linux-amd64 build-linux-arm64

package-amd64: build-linux-amd64
	mkdir -p $(DIST_DIR)/deskmon-agent
	cp $(BUILD_DIR)/$(BINARY)-linux-amd64 $(DIST_DIR)/deskmon-agent/$(BINARY)
	cp scripts/install.sh $(DIST_DIR)/deskmon-agent/
	chmod +x $(DIST_DIR)/deskmon-agent/install.sh
	cd $(DIST_DIR) && tar czf $(BINARY)-$(VERSION)-linux-amd64.tar.gz deskmon-agent/
	rm -rf $(DIST_DIR)/deskmon-agent
	@echo "Package: $(DIST_DIR)/$(BINARY)-$(VERSION)-linux-amd64.tar.gz"

package-arm64: build-linux-arm64
	mkdir -p $(DIST_DIR)/deskmon-agent
	cp $(BUILD_DIR)/$(BINARY)-linux-arm64 $(DIST_DIR)/deskmon-agent/$(BINARY)
	cp scripts/install.sh $(DIST_DIR)/deskmon-agent/
	chmod +x $(DIST_DIR)/deskmon-agent/install.sh
	cd $(DIST_DIR) && tar czf $(BINARY)-$(VERSION)-linux-arm64.tar.gz deskmon-agent/
	rm -rf $(DIST_DIR)/deskmon-agent
	@echo "Package: $(DIST_DIR)/$(BINARY)-$(VERSION)-linux-arm64.tar.gz"

package-all: package-amd64 package-arm64

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)

test:
	go test -v ./...

run:
	go run ./cmd/deskmon-agent
