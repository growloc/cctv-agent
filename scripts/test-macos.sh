#!/bin/bash

# CCTV Agent Test Script for macOS
set -e

# Configuration for macOS - Updated to use user directory
BINARY_NAME="cctv-agent"
INSTALL_DIR="$HOME/.cctv-agent"  # Changed from /usr/local/bin
CONFIG_DIR="$HOME/.cctv-agent"
LOG_DIR="$HOME/.cctv-agent/logs"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_info() {
    echo -e "${YELLOW}→${NC} $1"
}

# Check if Go is installed
if ! command -v go &> /dev/null; then
    print_error "Go is not installed. Please install Go first."
    exit 1
fi

# Build binary
print_info "Building CCTV Agent for macOS..."
go build -ldflags "-s -w" -o ${BINARY_NAME} .
print_success "Binary built successfully"

# Create directories first
print_info "Creating directories..."
mkdir -p ${INSTALL_DIR}
mkdir -p ${CONFIG_DIR}
mkdir -p ${LOG_DIR}
print_success "Directories created"

# Install binary (no sudo needed now)
print_info "Installing binary..."
cp ${BINARY_NAME} ${INSTALL_DIR}/${BINARY_NAME}
chmod +x ${INSTALL_DIR}/${BINARY_NAME}
print_success "Binary installed to ${INSTALL_DIR}/${BINARY_NAME}"

# Generate config
print_info "Generating configuration..."
${INSTALL_DIR}/${BINARY_NAME} --generate-config --config ${CONFIG_DIR}/config.json
print_success "Configuration generated at ${CONFIG_DIR}/config.json"

echo ""
echo "====================================="
echo "   CCTV Agent Ready for Testing"
echo "====================================="
echo ""
echo "To run manually:"
echo "  ${INSTALL_DIR}/${BINARY_NAME} --config ${CONFIG_DIR}/config.json"
echo ""
echo "To run in background:"
echo "  nohup ${INSTALL_DIR}/${BINARY_NAME} --config ${CONFIG_DIR}/config.json > ${LOG_DIR}/output.log 2>&1 &"
echo ""
echo "Config file: ${CONFIG_DIR}/config.json"
echo "Logs will be in: ${LOG_DIR}/"
echo ""
print_info "Remember to edit the configuration file before using!"