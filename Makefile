.PHONY: build build-linux-amd64 build-linux-arm64 build-all clean test run \
       package-amd64 package-arm64 package-all setup uninstall

VERSION := 0.1.0
BINARY := deskmon-agent
BUILD_DIR := bin
DIST_DIR := dist
PORT ?= 7654

# Detect system
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

# Map machine architecture to Go architecture
ifeq ($(UNAME_M),x86_64)
  GOARCH := amd64
else ifeq ($(UNAME_M),aarch64)
  GOARCH := arm64
else ifeq ($(UNAME_M),arm64)
  GOARCH := arm64
else
  GOARCH := $(UNAME_M)
endif

# Find Go binary — check PATH, then common install locations
# sudo doesn't inherit user PATH, so we search manually
GO := $(shell command -v go 2>/dev/null \
	|| (test -x /usr/local/go/bin/go && echo /usr/local/go/bin/go) \
	|| (test -x /usr/lib/go/bin/go && echo /usr/lib/go/bin/go) \
	|| (test -x /snap/go/current/bin/go && echo /snap/go/current/bin/go) \
	|| (test -x /home/*/go/bin/go && echo /home/*/go/bin/go) \
	|| (test -x $(HOME)/go/bin/go && echo $(HOME)/go/bin/go) \
	|| (test -x /opt/homebrew/bin/go && echo /opt/homebrew/bin/go) \
	|| echo "")

# ─────────────────────────────────────────────
# Setup: one command to build and install
# Usage: sudo make setup
#        sudo make setup PORT=9090
# ─────────────────────────────────────────────
setup:
	@if [ "$$(uname -s)" != "Linux" ]; then \
		echo "Error: setup must be run on a Linux server"; \
		echo ""; \
		echo "To cross-compile from macOS, use:"; \
		echo "  make package-amd64"; \
		echo "  make package-arm64"; \
		exit 1; \
	fi
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Error: setup requires root privileges"; \
		echo "  Run: sudo make setup"; \
		echo "  Or:  sudo make setup PORT=9090"; \
		exit 1; \
	fi
	@if [ -z "$(GO)" ]; then \
		echo "Error: Go is not installed"; \
		echo ""; \
		echo "Install Go with one of:"; \
		echo "  Ubuntu/Debian: sudo apt install golang-go"; \
		echo "  Fedora/RHEL:   sudo dnf install golang"; \
		echo "  Arch:          sudo pacman -S go"; \
		echo "  Manual:        https://go.dev/dl/"; \
		echo ""; \
		echo "Or cross-compile from another machine:"; \
		echo "  make package-amd64"; \
		echo "  make package-arm64"; \
		exit 1; \
	fi
	@echo "Detected: Linux $(UNAME_M) ($(GOARCH))"
	@echo "Go found: $(GO) ($$($(GO) version 2>/dev/null | awk '{print $$3}'))"
	@echo "Building $(BINARY) v$(VERSION)..."
	$(GO) build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY) ./cmd/deskmon-agent
	@echo "Build complete: $(BUILD_DIR)/$(BINARY)"
	@echo ""
	./scripts/install.sh --binary $(BUILD_DIR)/$(BINARY) --port $(PORT)

uninstall:
	@if [ "$$(id -u)" -ne 0 ]; then \
		echo "Error: uninstall requires root privileges"; \
		echo "  Run: sudo make uninstall"; \
		exit 1; \
	fi
	./scripts/install.sh --uninstall

# ─────────────────────────────────────────────
# Development targets
# ─────────────────────────────────────────────
build:
	$(or $(GO),go) build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY) ./cmd/deskmon-agent

build-linux-amd64:
	GOOS=linux GOARCH=amd64 $(or $(GO),go) build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64 ./cmd/deskmon-agent

build-linux-arm64:
	GOOS=linux GOARCH=arm64 $(or $(GO),go) build -ldflags "-X main.Version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64 ./cmd/deskmon-agent

build-all: build-linux-amd64 build-linux-arm64

# ─────────────────────────────────────────────
# Package targets (for remote deployment)
# ─────────────────────────────────────────────
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
	$(or $(GO),go) test -v ./...

run:
	$(or $(GO),go) run ./cmd/deskmon-agent
