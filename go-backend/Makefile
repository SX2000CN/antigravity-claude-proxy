# Antigravity Claude Proxy - Go Backend
# Build and development commands

# Binary names
BINARY_SERVER = antigravity-proxy
BINARY_ACCOUNTS = antigravity-accounts
BINARY_MIGRATE = antigravity-migrate

# Build directories
BUILD_DIR = build
CMD_DIR = cmd

# Go parameters
GOCMD = go
GOBUILD = $(GOCMD) build
GOCLEAN = $(GOCMD) clean
GOTEST = $(GOCMD) test
GOGET = $(GOCMD) get
GOMOD = $(GOCMD) mod

# Build flags
LDFLAGS = -ldflags="-s -w"
CGO_ENABLED = 0

# Detect OS
ifeq ($(OS),Windows_NT)
	EXT = .exe
	RM = del /Q
	MKDIR = mkdir
else
	EXT =
	RM = rm -f
	MKDIR = mkdir -p
endif

.PHONY: all build build-server build-accounts build-migrate clean deps test lint run dev help

# Default target
all: build

# Build all binaries
build: build-server build-accounts

# Build server binary
build-server:
	@echo "Building server..."
	CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_SERVER)$(EXT) ./$(CMD_DIR)/server

# Build accounts CLI binary
build-accounts:
	@echo "Building accounts CLI..."
	CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_ACCOUNTS)$(EXT) ./$(CMD_DIR)/accounts

# Build migration tool binary
build-migrate:
	@echo "Building migration tool..."
	CGO_ENABLED=$(CGO_ENABLED) $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_MIGRATE)$(EXT) ./$(CMD_DIR)/migrate

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	$(RM) $(BUILD_DIR)/$(BINARY_SERVER)$(EXT) 2>/dev/null || true
	$(RM) $(BUILD_DIR)/$(BINARY_ACCOUNTS)$(EXT) 2>/dev/null || true
	$(RM) $(BUILD_DIR)/$(BINARY_MIGRATE)$(EXT) 2>/dev/null || true

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race -cover ./...

# Run tests with coverage report
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run linter
lint:
	@echo "Running linter..."
	golangci-lint run ./...

# Run server in development mode
run:
	@echo "Starting server..."
	$(GOCMD) run ./$(CMD_DIR)/server --dev-mode

# Run server with hot reload (requires air)
dev:
	@echo "Starting server with hot reload..."
	air

# Run accounts CLI
accounts:
	@echo "Running accounts CLI..."
	$(GOCMD) run ./$(CMD_DIR)/accounts $(ARGS)

# Build for multiple platforms
build-all: build-linux build-darwin build-windows

build-linux:
	@echo "Building for Linux..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_SERVER)-linux-amd64 ./$(CMD_DIR)/server
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_ACCOUNTS)-linux-amd64 ./$(CMD_DIR)/accounts
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_SERVER)-linux-arm64 ./$(CMD_DIR)/server
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_ACCOUNTS)-linux-arm64 ./$(CMD_DIR)/accounts

build-darwin:
	@echo "Building for macOS..."
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_SERVER)-darwin-amd64 ./$(CMD_DIR)/server
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_ACCOUNTS)-darwin-amd64 ./$(CMD_DIR)/accounts
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_SERVER)-darwin-arm64 ./$(CMD_DIR)/server
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_ACCOUNTS)-darwin-arm64 ./$(CMD_DIR)/accounts

build-windows:
	@echo "Building for Windows..."
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_SERVER)-windows-amd64.exe ./$(CMD_DIR)/server
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_ACCOUNTS)-windows-amd64.exe ./$(CMD_DIR)/accounts

# Docker commands
docker-build:
	@echo "Building Docker image..."
	docker build -t antigravity-proxy-go .

docker-run:
	@echo "Running Docker container..."
	docker run -p 8080:8080 --rm antigravity-proxy-go

# Install tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/air-verse/air@latest

# Create build directory
$(BUILD_DIR):
	$(MKDIR) $(BUILD_DIR)

# Help
help:
	@echo "Antigravity Claude Proxy - Go Backend"
	@echo ""
	@echo "Usage:"
	@echo "  make build          Build all binaries"
	@echo "  make build-server   Build server binary"
	@echo "  make build-accounts Build accounts CLI binary"
	@echo "  make clean          Clean build artifacts"
	@echo "  make deps           Download dependencies"
	@echo "  make test           Run tests"
	@echo "  make test-coverage  Run tests with coverage report"
	@echo "  make lint           Run linter"
	@echo "  make run            Run server in dev mode"
	@echo "  make dev            Run server with hot reload"
	@echo "  make accounts       Run accounts CLI"
	@echo "  make build-all      Build for all platforms"
	@echo "  make docker-build   Build Docker image"
	@echo "  make docker-run     Run Docker container"
	@echo "  make install-tools  Install development tools"
	@echo "  make help           Show this help message"
