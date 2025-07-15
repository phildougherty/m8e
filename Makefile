# Makefile for Matey (m8e) - Kubernetes-native MCP server orchestrator
.PHONY: build clean test test-coverage test-race lint fmt vet security-scan docker-build help install uninstall dev deps clean-all

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

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	$(GO) test -v -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	$(GO) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "Coverage report generated: $(COVERAGE_DIR)/coverage.html"

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

# Run security scan (install gosec if not present)
security-scan:
	@echo "Running security scan..."
	@if ! command -v gosec >/dev/null 2>&1; then \
		echo "Installing gosec..."; \
		go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest; \
	fi
	gosec ./...

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

# Help
help:
	@echo "Matey (m8e) Build System"
	@echo "========================"
	@echo ""
	@echo "Build targets:"
	@echo "  build           - Build the application"
	@echo "  dev             - Build for development (with race detection)"
	@echo "  release         - Build optimized release version"
	@echo "  run             - Build and run the application"
	@echo ""
	@echo "Installation targets:"
	@echo "  install         - Install the application to $(INSTALL_BIN) (requires sudo)"
	@echo "  install-user    - Install the application to ~/.local/bin"
	@echo "  uninstall       - Uninstall the application"
	@echo "  deps            - Install/update dependencies"
	@echo ""
	@echo "Testing targets:"
	@echo "  test            - Run tests"
	@echo "  test-coverage   - Run tests with coverage"
	@echo "  test-race       - Run tests with race detection"
	@echo ""
	@echo "Quality targets:"
	@echo "  lint            - Run linter"
	@echo "  fmt             - Format code"
	@echo "  vet             - Run vet"
	@echo "  security-scan   - Run security scan"
	@echo "  quality         - Run all quality checks"
	@echo ""
	@echo "Docker targets:"
	@echo "  docker-build    - Build Docker image"
	@echo "  docker-push     - Push Docker image (requires DOCKER_REGISTRY)"
	@echo ""
	@echo "Maintenance targets:"
	@echo "  clean           - Clean build artifacts"
	@echo "  clean-all       - Clean everything including caches"
	@echo "  info            - Show project information"
	@echo "  help            - Show this help"
	@echo ""
	@echo "Examples:"
	@echo "  make build                    # Build the application"
	@echo "  make install                  # Install to $(INSTALL_BIN)"
	@echo "  make docker-build DOCKER_REGISTRY=my-registry.com"
	@echo "  make quality                  # Run all quality checks"

