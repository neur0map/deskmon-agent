#!/usr/bin/env bash
# Deskmon Agent — remote installer
# Usage: curl -fsSL https://raw.githubusercontent.com/neur0map/deskmon-agent/main/scripts/install-remote.sh | sudo bash
set -euo pipefail

REPO="neur0map/deskmon-agent"
INSTALL_TMP="$(mktemp -d)"
trap 'rm -rf "${INSTALL_TMP}"' EXIT

# Require root
if [[ $EUID -ne 0 ]]; then
    echo "Error: run this script with sudo"
    echo "  curl -fsSL ... | sudo bash"
    exit 1
fi

# Detect architecture
ARCH="$(uname -m)"
case "${ARCH}" in
    x86_64)  GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
    arm64)   GOARCH="arm64" ;;
    *)
        echo "Error: unsupported architecture: ${ARCH}"
        echo "  Supported: x86_64 (amd64), aarch64/arm64"
        exit 1
        ;;
esac

echo "Detected architecture: ${ARCH} (${GOARCH})"

# Fetch latest release tag
echo "Fetching latest release..."
LATEST_TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | cut -d'"' -f4)"

if [[ -z "${LATEST_TAG}" ]]; then
    echo "Error: could not determine latest release"
    echo "  Check https://github.com/${REPO}/releases"
    exit 1
fi

VERSION="${LATEST_TAG#v}"
echo "Latest version: ${VERSION} (${LATEST_TAG})"

# Download tarball
TARBALL="deskmon-agent-${VERSION}-linux-${GOARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${LATEST_TAG}/${TARBALL}"

echo "Downloading ${TARBALL}..."
curl -fsSL -o "${INSTALL_TMP}/${TARBALL}" "${URL}"

# Extract and install
echo "Extracting..."
tar xzf "${INSTALL_TMP}/${TARBALL}" -C "${INSTALL_TMP}"

echo "Installing..."
cd "${INSTALL_TMP}/deskmon-agent"
bash install.sh "$@"
