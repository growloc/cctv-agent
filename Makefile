# CCTV Agent Makefile

# Variables
BINARY_NAME=cctv-agent
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "1.0.0")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -s -w"
GOARCH_ARM=arm
GOARCH_ARM64=arm64
GOOS=linux

# Default target
.PHONY: all
all: build

# Build for current platform
.PHONY: build
build:
	@echo "Building ${BINARY_NAME}..."
	go build ${LDFLAGS} -o ${BINARY_NAME} .

# Build for Raspberry Pi (ARM)
.PHONY: build-pi
build-pi:
	@echo "Building for Raspberry Pi (ARM)..."
	GOOS=${GOOS} GOARCH=${GOARCH_ARM} GOARM=7 go build ${LDFLAGS} -o ${BINARY_NAME}-pi .

# Build for Raspberry Pi 64-bit (ARM64)
.PHONY: build-pi64
build-pi64:
	@echo "Building for Raspberry Pi 64-bit (ARM64)..."
	GOOS=${GOOS} GOARCH=${GOARCH_ARM64} go build ${LDFLAGS} -o ${BINARY_NAME}-pi64 .

# Build all platforms
.PHONY: build-all
build-all: build build-pi build-pi64

# Run the application
.PHONY: run
run: build
	./${BINARY_NAME}

# Generate sample configuration
.PHONY: config
config: build
	./${BINARY_NAME} --generate-config

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning..."
	rm -f ${BINARY_NAME} ${BINARY_NAME}-pi ${BINARY_NAME}-pi64
	go clean

# Install dependencies
.PHONY: deps
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

# Update dependencies
.PHONY: update-deps
update-deps:
	@echo "Updating dependencies..."
	go get -u ./...
	go mod tidy

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -cover ./...

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	go fmt ./...
	gofmt -s -w .

# Lint code
.PHONY: lint
lint:
	@echo "Linting code..."
	golangci-lint run

# Install the binary
.PHONY: install
install: build
	@echo "Installing ${BINARY_NAME}..."
	sudo cp ${BINARY_NAME} /usr/local/bin/
	sudo chmod +x /usr/local/bin/${BINARY_NAME}
	@echo "Creating config directory..."
	sudo mkdir -p /etc/cctv-agent
	@echo "Generating default config..."
	sudo /usr/local/bin/${BINARY_NAME} --generate-config --config /etc/cctv-agent/config.json
	@echo "Installation complete!"

# Install systemd service
.PHONY: install-service
install-service: install
	@echo "Installing systemd service..."
	sudo cp scripts/cctv-agent.service /etc/systemd/system/
	sudo systemctl daemon-reload
	sudo systemctl enable cctv-agent
	@echo "Service installed. Start with: sudo systemctl start cctv-agent"

# Uninstall
.PHONY: uninstall
uninstall:
	@echo "Uninstalling ${BINARY_NAME}..."
	sudo systemctl stop cctv-agent 2>/dev/null || true
	sudo systemctl disable cctv-agent 2>/dev/null || true
	sudo rm -f /etc/systemd/system/cctv-agent.service
	sudo rm -f /usr/local/bin/${BINARY_NAME}
	sudo rm -rf /etc/cctv-agent
	sudo systemctl daemon-reload
	@echo "Uninstallation complete!"

# Show version
.PHONY: version
version:
	@echo ${VERSION}

# Help
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make build          - Build for current platform"
	@echo "  make build-pi       - Build for Raspberry Pi (ARM)"
	@echo "  make build-pi64     - Build for Raspberry Pi 64-bit (ARM64)"
	@echo "  make build-all      - Build for all platforms"
	@echo "  make run            - Build and run the application"
	@echo "  make config         - Generate sample configuration"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make deps           - Install dependencies"
	@echo "  make update-deps    - Update dependencies"
	@echo "  make test           - Run tests"
	@echo "  make test-coverage  - Run tests with coverage"
	@echo "  make fmt            - Format code"
	@echo "  make lint           - Lint code"
	@echo "  make install        - Install the binary"
	@echo "  make install-service- Install systemd service"
	@echo "  make uninstall      - Uninstall the binary and service"
	@echo "  make version        - Show version"
	@echo "  make help           - Show this help message"
