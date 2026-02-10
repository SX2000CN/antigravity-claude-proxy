#!/bin/bash
#
# Antigravity Claude Proxy - Installation Script
# This script installs the Go backend on Linux systems
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/.../install.sh | bash
#   # or
#   ./install.sh
#

set -e

# ===========================================
# Configuration
# ===========================================
INSTALL_DIR="/opt/antigravity-proxy"
SERVICE_NAME="antigravity-proxy"
BINARY_NAME="antigravity-proxy"
CONFIG_DIR="$HOME/.config/antigravity-proxy"
REPO_URL="https://github.com/badri-s2001/antigravity-claude-proxy"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# ===========================================
# Helper functions
# ===========================================
info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        error "This script must be run as root (use sudo)"
    fi
}

check_dependencies() {
    info "Checking dependencies..."

    # Check for required tools
    for cmd in curl wget git; do
        if ! command -v $cmd &> /dev/null; then
            warn "$cmd not found, installing..."
            apt-get update && apt-get install -y $cmd || yum install -y $cmd
        fi
    done

    success "Dependencies OK"
}

# ===========================================
# Installation
# ===========================================
install_binary() {
    info "Installing binary..."

    # Create install directory
    mkdir -p "$INSTALL_DIR"
    mkdir -p "$INSTALL_DIR/public"

    # Determine architecture
    ARCH=$(uname -m)
    case $ARCH in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64)
            ARCH="arm64"
            ;;
        *)
            error "Unsupported architecture: $ARCH"
            ;;
    esac

    # Check if binary exists in current directory
    if [[ -f "./go-backend/build/${BINARY_NAME}-linux-${ARCH}" ]]; then
        info "Using local binary..."
        cp "./go-backend/build/${BINARY_NAME}-linux-${ARCH}" "$INSTALL_DIR/$BINARY_NAME"
    elif [[ -f "./go-backend/build/${BINARY_NAME}" ]]; then
        info "Using local binary..."
        cp "./go-backend/build/${BINARY_NAME}" "$INSTALL_DIR/$BINARY_NAME"
    else
        error "Binary not found. Please build first: cd go-backend && make build-linux"
    fi

    # Copy public directory
    if [[ -d "./public" ]]; then
        info "Copying frontend assets..."
        cp -r ./public/* "$INSTALL_DIR/public/"
    else
        warn "public directory not found, skipping..."
    fi

    # Set permissions
    chmod +x "$INSTALL_DIR/$BINARY_NAME"

    success "Binary installed to $INSTALL_DIR"
}

install_service() {
    info "Installing systemd service..."

    # Copy service file
    if [[ -f "./go-backend/deploy/antigravity-proxy.service" ]]; then
        cp ./go-backend/deploy/antigravity-proxy.service /etc/systemd/system/
    else
        # Create service file
        cat > /etc/systemd/system/antigravity-proxy.service << 'EOF'
[Unit]
Description=Antigravity Claude Proxy Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/antigravity-proxy
ExecStart=/opt/antigravity-proxy/antigravity-proxy --strategy=hybrid
Restart=always
RestartSec=5
Environment=PORT=8080
Environment=HOST=0.0.0.0

[Install]
WantedBy=multi-user.target
EOF
    fi

    # Reload systemd
    systemctl daemon-reload

    success "Systemd service installed"
}

create_config_dir() {
    info "Creating config directory..."

    mkdir -p "$CONFIG_DIR"

    # Create default config if not exists
    if [[ ! -f "$CONFIG_DIR/config.json" ]]; then
        cat > "$CONFIG_DIR/config.json" << 'EOF'
{
  "apiKey": "",
  "webuiPassword": "",
  "devMode": false,
  "maxRetries": 5,
  "maxAccounts": 100,
  "accountSelection": {
    "strategy": "hybrid"
  },
  "port": 8080,
  "host": "0.0.0.0"
}
EOF
        success "Default config created at $CONFIG_DIR/config.json"
    else
        info "Config already exists, skipping..."
    fi
}

start_service() {
    info "Starting service..."

    systemctl enable "$SERVICE_NAME"
    systemctl start "$SERVICE_NAME"

    # Wait for service to start
    sleep 2

    if systemctl is-active --quiet "$SERVICE_NAME"; then
        success "Service started successfully!"
    else
        error "Service failed to start. Check: journalctl -u $SERVICE_NAME"
    fi
}

print_status() {
    echo ""
    echo "============================================"
    echo "  Antigravity Claude Proxy - Installed!"
    echo "============================================"
    echo ""
    echo "  Service Status: $(systemctl is-active $SERVICE_NAME)"
    echo "  Install Dir:    $INSTALL_DIR"
    echo "  Config Dir:     $CONFIG_DIR"
    echo ""
    echo "  Commands:"
    echo "    sudo systemctl status $SERVICE_NAME"
    echo "    sudo systemctl restart $SERVICE_NAME"
    echo "    sudo journalctl -u $SERVICE_NAME -f"
    echo ""
    echo "  Access:"
    echo "    http://localhost:8080"
    echo ""
    echo "============================================"
}

# ===========================================
# Uninstall
# ===========================================
uninstall() {
    info "Uninstalling Antigravity Claude Proxy..."

    # Stop and disable service
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true
    systemctl disable "$SERVICE_NAME" 2>/dev/null || true

    # Remove service file
    rm -f /etc/systemd/system/antigravity-proxy.service
    systemctl daemon-reload

    # Remove installation directory
    rm -rf "$INSTALL_DIR"

    success "Uninstalled successfully!"
    warn "Config directory preserved: $CONFIG_DIR"
    warn "To remove config: rm -rf $CONFIG_DIR"
}

# ===========================================
# Main
# ===========================================
main() {
    echo ""
    echo "============================================"
    echo "  Antigravity Claude Proxy Installer"
    echo "============================================"
    echo ""

    case "${1:-install}" in
        install)
            check_root
            check_dependencies
            install_binary
            install_service
            create_config_dir
            start_service
            print_status
            ;;
        uninstall)
            check_root
            uninstall
            ;;
        *)
            echo "Usage: $0 {install|uninstall}"
            exit 1
            ;;
    esac
}

main "$@"
