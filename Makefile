# Makefile for Matey (m8e) - Kubernetes-native MCP server orchestrator
.PHONY: build clean test test-coverage test-race lint fmt vet security-scan docker-build help install uninstall dev deps clean-all build-voice install-voice

# Build variables
BINARY_NAME=matey
GO=go
BUILD_DIR=bin
ALT_BUILD_DIR=build
SRC_MAIN=./cmd/matey
COVERAGE_DIR=coverage
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)"

# Installation paths
INSTALL_PREFIX=/usr/local
INSTALL_BIN=$(INSTALL_PREFIX)/bin
INSTALL_MAN=$(INSTALL_PREFIX)/share/man/man1

# Docker variables
DOCKER_REGISTRY=
DOCKER_IMAGE=matey
DOCKER_TAG=latest

# Default target
all: build

# Build the application
build:
	@echo "Building matey $(VERSION) ($(COMMIT))..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(SRC_MAIN)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"
	@echo "Version: $(VERSION)"

# Build for development (with race detection and debug symbols)
dev:
	@echo "Building matey for development..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -race -gcflags="-N -l" $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(SRC_MAIN)
	@echo "Development build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Build with voice features enabled
build-voice:
	@echo "Building matey $(VERSION) with voice features..."
	@echo "Checking PortAudio installation..."
	@if ! pkg-config --exists portaudio-2.0; then \
		echo "Error: PortAudio not found. Please install it first:"; \
		echo "  Ubuntu/Debian: sudo apt install portaudio19-dev pkg-config"; \
		echo "  macOS: brew install portaudio pkg-config"; \
		echo "  Or run: ./scripts/build-with-voice.sh"; \
		exit 1; \
	fi
	@echo "PortAudio found: $$(pkg-config --modversion portaudio-2.0)"
	@mkdir -p $(BUILD_DIR)
	$(GO) build -tags voice $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(SRC_MAIN)
	@echo "Voice-enabled build complete: $(BUILD_DIR)/$(BINARY_NAME)"
	@echo "Version: $(VERSION)"
	@echo ""
	@echo "Voice features enabled! Usage:"
	@echo "  export VOICE_ENABLED=true"
	@echo "  export TTS_ENDPOINT=http://localhost:8000/v1/audio/speech"
	@echo "  ./$(BUILD_DIR)/$(BINARY_NAME) chat"

# Install dependencies
deps:
	@echo "Installing dependencies..."
	$(GO) mod download
	$(GO) mod verify
	@echo "Dependencies installed"

# Install the application
install: build
	@echo "Installing matey to $(INSTALL_BIN)..."
	@if [ ! -w $(INSTALL_PREFIX) ]; then \
		echo "Error: $(INSTALL_PREFIX) is not writable. Try:"; \
		echo "  sudo make install"; \
		echo "  OR"; \
		echo "  make install-user"; \
		exit 1; \
	fi
	@mkdir -p $(INSTALL_BIN)
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_BIN)/$(BINARY_NAME)
	@chmod +x $(INSTALL_BIN)/$(BINARY_NAME)
	@echo "Installation complete: $(INSTALL_BIN)/$(BINARY_NAME)"
	@echo "Run 'matey --help' to get started"

# Install the application to user's home directory
install-user: build
	@echo "Installing matey to ~/.local/bin..."
	@mkdir -p ~/.local/bin
	@cp $(BUILD_DIR)/$(BINARY_NAME) ~/.local/bin/$(BINARY_NAME)
	@chmod +x ~/.local/bin/$(BINARY_NAME)
	@echo "Installation complete: ~/.local/bin/$(BINARY_NAME)"
	@echo "Make sure ~/.local/bin is in your PATH:"
	@echo "  export PATH=~/.local/bin:\$$PATH"
	@echo "Run 'matey --help' to get started"

# Install the voice-enabled application
install-voice: build-voice
	@echo "Installing voice-enabled matey to $(INSTALL_BIN)..."
	@if [ ! -w $(INSTALL_PREFIX) ]; then \
		echo "Error: $(INSTALL_PREFIX) is not writable. Installing to ~/.local/bin instead..."; \
		mkdir -p ~/.local/bin; \
		cp $(BUILD_DIR)/$(BINARY_NAME) ~/.local/bin/$(BINARY_NAME); \
		chmod +x ~/.local/bin/$(BINARY_NAME); \
		echo "Installation complete: ~/.local/bin/$(BINARY_NAME)"; \
		echo "Make sure ~/.local/bin is in your PATH:"; \
		echo "  export PATH=~/.local/bin:\$$PATH"; \
	else \
		mkdir -p $(INSTALL_BIN); \
		cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_BIN)/$(BINARY_NAME); \
		chmod +x $(INSTALL_BIN)/$(BINARY_NAME); \
		echo "Installation complete: $(INSTALL_BIN)/$(BINARY_NAME)"; \
	fi
	@echo "Voice features are enabled! Setup:"
	@echo "  export VOICE_ENABLED=true"
	@echo "  export TTS_ENDPOINT=http://localhost:8000/v1/audio/speech"
	@echo "  # Install Whisper: pip install faster-whisper"
	@echo "  matey chat"

# Uninstall the application
uninstall:
	@echo "Uninstalling matey..."
	@rm -f $(INSTALL_BIN)/$(BINARY_NAME)
	@rm -f ~/.local/bin/$(BINARY_NAME)
	@echo "Uninstallation complete"

# Run tests
test:
	@echo "Running tests..."
	$(GO) test ./...

# Run tests with coverage using comprehensive script
test-coverage:
	@echo "Running comprehensive test coverage analysis..."
	@./scripts/test-coverage.sh all

# Run unit tests with coverage only
test-coverage-unit:
	@echo "Running unit tests with coverage..."
	@./scripts/test-coverage.sh unit

# Run integration tests with coverage
test-coverage-integration:
	@echo "Running integration tests with coverage..."
	@./scripts/test-coverage.sh integration

# Run tests with race detection
test-race:
	@echo "Running tests with race detection..."
	$(GO) test -race ./...

# Run linter (install golangci-lint if not present)
lint:
	@echo "Running linter..."
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi
	golangci-lint run

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Run vet
vet:
	@echo "Running vet..."
	$(GO) vet ./...

# Run comprehensive security scan
security-scan:
	@echo "Running comprehensive security scan..."
	@./scripts/security-scan.sh all

# Run specific security scans
security-scan-gosec:
	@echo "Running gosec security scan..."
	@./scripts/security-scan.sh gosec

security-scan-vulns:
	@echo "Running vulnerability scan..."
	@./scripts/security-scan.sh vulns

security-scan-deps:
	@echo "Running dependency audit..."
	@./scripts/security-scan.sh deps

security-scan-secrets:
	@echo "Running secrets scan..."
	@./scripts/security-scan.sh secrets

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	@if [ -n "$(DOCKER_REGISTRY)" ]; then \
		docker tag $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG); \
		echo "Tagged as $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)"; \
	fi
	@echo "Docker build complete"

# Push Docker image
docker-push: docker-build
	@if [ -z "$(DOCKER_REGISTRY)" ]; then \
		echo "Error: DOCKER_REGISTRY not set"; \
		exit 1; \
	fi
	@echo "Pushing Docker image..."
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)
	@echo "Docker push complete"

# Run all quality checks
quality: fmt vet lint test-race test-coverage security-scan

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR) $(ALT_BUILD_DIR) $(COVERAGE_DIR)
	@echo "Clean complete"

# Clean everything (including Go module cache)
clean-all: clean
	@echo "Cleaning all artifacts and caches..."
	@$(GO) clean -modcache -cache -testcache
	@echo "Deep clean complete"

# Show project info
info:
	@echo "Matey (m8e) - Kubernetes-native MCP server orchestrator"
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Go version: $(shell go version)"
	@echo "Build directory: $(BUILD_DIR)"
	@echo "Install directory: $(INSTALL_BIN)"
	@echo ""
	@echo "Dependencies:"
	@$(GO) list -m all | head -10

# Run the application (for development)
run: build
	@echo "Running matey..."
	@./$(BUILD_DIR)/$(BINARY_NAME)

# Release build (optimized, no debug symbols)
release:
	@echo "Building release version $(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -trimpath $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(SRC_MAIN)
	@echo "Release build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Prepare release interactively
prepare-release:
	@echo "Starting interactive release preparation..."
	@./scripts/prepare-release.sh interactive

# Validate release readiness
validate-release:
	@echo "Validating release readiness..."
	@./scripts/prepare-release.sh checks

# Generate changelog
changelog:
	@echo "Generating changelog..."
	@./scripts/prepare-release.sh changelog

# Generate comprehensive documentation
docs:
	@echo "Generating comprehensive documentation..."
	@./scripts/generate-docs.sh all

# Generate API documentation
docs-api:
	@echo "Generating API documentation..."
	@./scripts/generate-docs.sh specs

# Generate client examples
docs-examples:
	@echo "Generating client examples..."
	@./scripts/generate-docs.sh examples

# Help
help:
	@echo "Matey (m8e) Build System"
	@echo "========================"
	@echo ""
	@echo "Build targets:"
	@echo "  build           - Build the application"
	@echo "  build-voice     - Build with voice features enabled (requires PortAudio)"
	@echo "  dev             - Build for development (with race detection)"
	@echo "  release         - Build optimized release version"
	@echo "  run             - Build and run the application"
	@echo ""
	@echo "Installation targets:"
	@echo "  install         - Install the application to $(INSTALL_BIN) (requires sudo)"
	@echo "  install-user    - Install the application to ~/.local/bin"
	@echo "  install-voice   - Install voice-enabled version (requires PortAudio)"
	@echo "  uninstall       - Uninstall the application"
	@echo "  deps            - Install/update dependencies"
	@echo ""
	@echo "Testing targets:"
	@echo "  test                     - Run basic tests"
	@echo "  test-coverage            - Run comprehensive test coverage analysis"
	@echo "  test-coverage-unit       - Run unit tests with coverage only"
	@echo "  test-coverage-integration - Run integration tests with coverage"
	@echo "  test-race                - Run tests with race detection"
	@echo ""
	@echo "Quality targets:"
	@echo "  lint                 - Run linter"
	@echo "  fmt                  - Format code"
	@echo "  vet                  - Run vet"
	@echo "  security-scan        - Run comprehensive security scan"
	@echo "  security-scan-gosec  - Run gosec security scan only"
	@echo "  security-scan-vulns  - Run vulnerability scan only"
	@echo "  security-scan-deps   - Run dependency audit only"
	@echo "  security-scan-secrets - Run secrets scan only"
	@echo "  quality              - Run all quality checks"
	@echo ""
	@echo "Docker targets:"
	@echo "  docker-build    - Build Docker image"
	@echo "  docker-push     - Push Docker image (requires DOCKER_REGISTRY)"
	@echo ""
	@echo "Release targets:"
	@echo "  release         - Build optimized release version"
	@echo "  prepare-release - Interactive release preparation"
	@echo "  validate-release - Validate release readiness"
	@echo "  changelog       - Generate changelog"
	@echo ""
	@echo "Documentation targets:"
	@echo "  docs            - Generate comprehensive documentation"
	@echo "  docs-api        - Generate API documentation and specs"
	@echo "  docs-examples   - Generate client examples"
	@echo ""
	@echo "Maintenance targets:"
	@echo "  clean           - Clean build artifacts"
	@echo "  clean-all       - Clean everything including caches"
	@echo "  info            - Show project information"
	@echo "  help            - Show this help"
	@echo ""
	@echo "Examples:"
	@echo "  make build                    # Build the application"
	@echo "  make build-voice              # Build with voice features"
	@echo "  make install                  # Install to $(INSTALL_BIN)"
	@echo "  make install-voice            # Install voice-enabled version"
	@echo "  make docker-build DOCKER_REGISTRY=my-registry.com"
	@echo "  make quality                  # Run all quality checks"

