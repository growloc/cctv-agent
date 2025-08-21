#!/bin/bash

# CCTV Agent Installation Script
# This script installs the CCTV Agent on a Raspberry Pi

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
BINARY_NAME="cctv-agent"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/cctv-agent"
LOG_DIR="/var/log/cctv-agent"
SERVICE_FILE="/etc/systemd/system/cctv-agent.service"

# Functions
print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_info() {
    echo -e "${YELLOW}→${NC} $1"
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        print_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

check_architecture() {
    ARCH=$(uname -m)
    case $ARCH in
        armv7l)
            BINARY_SUFFIX="-pi"
            print_info "Detected Raspberry Pi (32-bit ARM)"
            ;;
        aarch64)
            BINARY_SUFFIX="-pi64"
            print_info "Detected Raspberry Pi (64-bit ARM)"
            ;;
        x86_64)
            BINARY_SUFFIX=""
            print_info "Detected x86_64 architecture"
            ;;
        *)
            print_error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac
}

install_dependencies() {
    print_info "Installing dependencies..."
    apt-get update
    apt-get install -y ffmpeg
    print_success "Dependencies installed"
}

build_binary() {
    print_info "Building CCTV Agent..."
    
    # Check if Go is installed
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed. Please install Go first."
        exit 1
    fi
    
    # Build the binary
    if [ "$BINARY_SUFFIX" == "-pi" ]; then
        GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-s -w" -o ${BINARY_NAME}${BINARY_SUFFIX} .
    elif [ "$BINARY_SUFFIX" == "-pi64" ]; then
        GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o ${BINARY_NAME}${BINARY_SUFFIX} .
    else
        go build -ldflags "-s -w" -o ${BINARY_NAME} .
    fi
    
    print_success "Binary built successfully"
}

install_binary() {
    print_info "Installing binary..."
    
    # Copy binary to install directory
    if [ -n "$BINARY_SUFFIX" ]; then
        cp ${BINARY_NAME}${BINARY_SUFFIX} ${INSTALL_DIR}/${BINARY_NAME}
    else
        cp ${BINARY_NAME} ${INSTALL_DIR}/${BINARY_NAME}
    fi
    
    chmod +x ${INSTALL_DIR}/${BINARY_NAME}
    print_success "Binary installed to ${INSTALL_DIR}/${BINARY_NAME}"
}

create_directories() {
    print_info "Creating directories..."
    
    # Create config directory
    mkdir -p ${CONFIG_DIR}
    
    # Create log directory
    mkdir -p ${LOG_DIR}
    
    # Set permissions
    chown -R pi:pi ${CONFIG_DIR}
    chown -R pi:pi ${LOG_DIR}
    
    print_success "Directories created"
}

generate_config() {
    print_info "Generating configuration..."
    
    # Generate default configuration
    ${INSTALL_DIR}/${BINARY_NAME} --generate-config --config ${CONFIG_DIR}/config.json
    
    # Set permissions
    chown pi:pi ${CONFIG_DIR}/config.json
    chmod 644 ${CONFIG_DIR}/config.json
    
    print_success "Configuration generated at ${CONFIG_DIR}/config.json"
    print_info "Please edit the configuration file to match your setup"
}

install_service() {
    print_info "Installing systemd service..."
    
    # Copy service file
    cp scripts/cctv-agent.service ${SERVICE_FILE}
    
    # Reload systemd
    systemctl daemon-reload
    
    # Enable service
    systemctl enable cctv-agent
    
    print_success "Service installed and enabled"
}

start_service() {
    print_info "Starting CCTV Agent service..."
    systemctl start cctv-agent
    
    # Wait a moment and check status
    sleep 2
    if systemctl is-active --quiet cctv-agent; then
        print_success "CCTV Agent service is running"
    else
        print_error "Failed to start CCTV Agent service"
        print_info "Check logs with: journalctl -u cctv-agent -f"
        exit 1
    fi
}

print_summary() {
    echo ""
    echo "======================================"
    echo "   CCTV Agent Installation Complete"
    echo "======================================"
    echo ""
    echo "Installation details:"
    echo "  Binary: ${INSTALL_DIR}/${BINARY_NAME}"
    echo "  Config: ${CONFIG_DIR}/config.json"
    echo "  Logs:   ${LOG_DIR}/"
    echo "  Service: cctv-agent.service"
    echo ""
    echo "Useful commands:"
    echo "  Start service:   sudo systemctl start cctv-agent"
    echo "  Stop service:    sudo systemctl stop cctv-agent"
    echo "  Restart service: sudo systemctl restart cctv-agent"
    echo "  View status:     sudo systemctl status cctv-agent"
    echo "  View logs:       sudo journalctl -u cctv-agent -f"
    echo "  Edit config:     sudo nano ${CONFIG_DIR}/config.json"
    echo ""
    print_info "Remember to edit the configuration file before using!"
}

# Main installation flow
main() {
    echo "======================================"
    echo "   CCTV Agent Installation Script"
    echo "======================================"
    echo ""
    
    check_root
    check_architecture
    install_dependencies
    build_binary
    install_binary
    create_directories
    generate_config
    install_service
    
    # Ask if user wants to start the service now
    read -p "Do you want to start the CCTV Agent service now? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        start_service
    else
        print_info "You can start the service later with: sudo systemctl start cctv-agent"
    fi
    
    print_summary
}

# Run main function
main
