#!/usr/bin/env bash
set -euo pipefail

BINARY_NAME="deskmon-agent"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/deskmon"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"
SERVICE_NAME="deskmon-agent"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

DEFAULT_PORT=7654
PORT="${DEFAULT_PORT}"
BINARY_PATH=""
UNINSTALL=false

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --port PORT      Set the listening port (default: ${DEFAULT_PORT})"
    echo "  --binary PATH    Path to the built binary (default: auto-detect)"
    echo "  --uninstall      Remove deskmon-agent and all configuration"
    echo "  --help           Show this help message"
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --port)
            PORT="$2"
            shift 2
            ;;
        --binary)
            BINARY_PATH="$2"
            shift 2
            ;;
        --uninstall)
            UNINSTALL=true
            shift
            ;;
        --help)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# Check for root
if [[ $EUID -ne 0 ]]; then
    echo "Error: This script must be run as root (use sudo)"
    exit 1
fi

do_uninstall() {
    echo "Uninstalling ${BINARY_NAME}..."

    if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
        systemctl stop "${SERVICE_NAME}"
        echo "  Stopped service"
    fi

    if systemctl is-enabled --quiet "${SERVICE_NAME}" 2>/dev/null; then
        systemctl disable "${SERVICE_NAME}"
        echo "  Disabled service"
    fi

    if [[ -f "${SERVICE_FILE}" ]]; then
        rm -f "${SERVICE_FILE}"
        systemctl daemon-reload
        echo "  Removed service file"
    fi

    if [[ -f "${INSTALL_DIR}/${BINARY_NAME}" ]]; then
        rm -f "${INSTALL_DIR}/${BINARY_NAME}"
        echo "  Removed binary"
    fi

    if [[ -d "${CONFIG_DIR}" ]]; then
        rm -rf "${CONFIG_DIR}"
        echo "  Removed config directory"
    fi

    echo ""
    echo "deskmon-agent has been completely removed."
    exit 0
}

find_binary() {
    # If --binary was provided, use that
    if [[ -n "${BINARY_PATH}" ]]; then
        if [[ -f "${BINARY_PATH}" ]]; then
            echo "${BINARY_PATH}"
            return
        fi
        echo "Error: binary not found at ${BINARY_PATH}" >&2
        exit 1
    fi

    # Auto-detect: check common locations relative to script
    local SCRIPT_DIR
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
    local REPO_DIR
    REPO_DIR="$(cd "${SCRIPT_DIR}/.." 2>/dev/null && pwd || echo "")"

    # Check bin/ in repo root (built by make setup)
    if [[ -n "${REPO_DIR}" && -f "${REPO_DIR}/bin/${BINARY_NAME}" ]]; then
        echo "${REPO_DIR}/bin/${BINARY_NAME}"
        return
    fi

    # Check same directory as script (package deployment)
    if [[ -f "${SCRIPT_DIR}/${BINARY_NAME}" ]]; then
        echo "${SCRIPT_DIR}/${BINARY_NAME}"
        return
    fi

    echo "Error: ${BINARY_NAME} binary not found" >&2
    echo "  Run 'make build' first, or pass --binary /path/to/${BINARY_NAME}" >&2
    exit 1
}

do_install() {
    local FOUND_BINARY
    FOUND_BINARY="$(find_binary)"

    echo "Installing ${BINARY_NAME}..."
    echo "  Binary: ${FOUND_BINARY}"

    # Stop existing service if running
    if systemctl is-active --quiet "${SERVICE_NAME}" 2>/dev/null; then
        systemctl stop "${SERVICE_NAME}"
        echo "  Stopped existing service"
    fi

    # Install binary
    cp "${FOUND_BINARY}" "${INSTALL_DIR}/${BINARY_NAME}"
    chmod 755 "${INSTALL_DIR}/${BINARY_NAME}"
    echo "  Installed binary to ${INSTALL_DIR}/${BINARY_NAME}"

    # Create config directory
    mkdir -p "${CONFIG_DIR}"
    chmod 700 "${CONFIG_DIR}"

    # Generate config if it doesn't exist (preserve existing config on upgrades)
    if [[ -f "${CONFIG_FILE}" ]]; then
        echo "  Existing config preserved at ${CONFIG_FILE}"
        # Read existing port for display
        EXISTING_PORT=$(grep 'port:' "${CONFIG_FILE}" | awk '{print $2}')
        PORT="${EXISTING_PORT:-${PORT}}"
    else
        cat > "${CONFIG_FILE}" <<CONF
port: ${PORT}
bind: "127.0.0.1"
CONF
        chmod 600 "${CONFIG_FILE}"
        echo "  Config written to ${CONFIG_FILE}"
    fi

    # Create systemd service file
    cat > "${SERVICE_FILE}" <<SERVICE
[Unit]
Description=Deskmon Agent - System Monitoring
After=network.target docker.service
Wants=docker.service

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/${BINARY_NAME}
Restart=always
RestartSec=5
User=root

# Security hardening
ProtectSystem=strict
ReadOnlyPaths=/
ProtectHome=yes
NoNewPrivileges=yes
PrivateTmp=yes

# Allow reading system stats and Docker socket
ReadWritePaths=/var/run/docker.sock

[Install]
WantedBy=multi-user.target
SERVICE
    echo "  Service file created"

    # Reload systemd, enable and start
    systemctl daemon-reload
    systemctl enable "${SERVICE_NAME}" --quiet
    systemctl start "${SERVICE_NAME}"
    echo "  Service enabled and started"

    # Get server IP for display
    local SERVER_IP
    SERVER_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "<server-ip>")

    # Print summary
    echo ""
    echo "==========================================="
    echo "  deskmon-agent installed successfully"
    echo "==========================================="
    echo ""
    echo "  Listening: 127.0.0.1:${PORT} (localhost only)"
    echo "  Config:    ${CONFIG_FILE}"
    echo "  Service:   ${SERVICE_NAME}"
    echo ""
    echo "  The agent binds to localhost only."
    echo "  The Deskmon macOS app connects via SSH tunnel."
    echo ""
    echo "  Add this server in the macOS app:"
    echo "    Host:     ${SERVER_IP}"
    echo "    SSH User: $(logname 2>/dev/null || echo '<your-user>')"
    echo ""
    echo "  Useful commands:"
    echo "    systemctl status ${SERVICE_NAME}"
    echo "    journalctl -u ${SERVICE_NAME} -f"
    echo "==========================================="
}

if [[ "${UNINSTALL}" == "true" ]]; then
    do_uninstall
else
    do_install
fi
