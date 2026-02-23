# Agent Go Build Configuration

.PHONY: help build test docker-build docker-run clean lint verify-no-tests \
        release release-all release-linux-amd64 release-linux-arm64 release-darwin-amd64 \
        release-darwin-arm64 release-windows-amd64 checksums package dist clean-dist \
        a2a a2a-stop a2a-status a2a-test a2a-logs a2a-restart

# Default target
help:
	@echo "Available targets:"
	@echo ""
	@echo "Development:"
	@echo "  build           - Build the binary locally (CGO)"
	@echo "  run             - Run development server (default port 4015)"
	@echo "  test            - Run all tests"
	@echo "  lint            - Run linters"
	@echo ""
	@echo "Docker:"
	@echo "  docker-build    - Build Docker image"
	@echo "  docker-run      - Run Docker container"
	@echo ""
	@echo "Distribution (cross-platform binaries):"
	@echo "  release         - Build Linux + Windows (works from any OS)"
	@echo "  release-all     - Build all platforms (macOS needs native build)"
	@echo "  dist            - Build, checksum, and package all platforms"
	@echo "  clean-dist      - Remove dist/ folder"
	@echo ""
	@echo "A2A Multi-Agent:"
	@echo "  a2a             - Start two A2A peer agents (ports 4015+4016)"
	@echo "  a2a-stop        - Stop both agents"
	@echo "  a2a-test        - Run A2A protocol tests against running pair"
	@echo "  a2a-status      - Check agent health and discovery"
	@echo "  a2a-logs        - Tail both agent logs"
	@echo "  a2a-restart     - Restart both agents"
	@echo ""
	@echo "Individual platforms:"
	@echo "  release-linux-amd64   - Linux x86_64"
	@echo "  release-linux-arm64   - Linux ARM64"
	@echo "  release-darwin-amd64  - macOS Intel (native only)"
	@echo "  release-darwin-arm64  - macOS Apple Silicon (native only)"
	@echo "  release-windows-amd64 - Windows x86_64"

# Build binary locally
build:
	@echo "Building agent-core..."
	CGO_ENABLED=1 go build -o agent-core .

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Build Docker image (production)
docker-build:
	@echo "Building Docker image (production - no tests)..."
	docker build -t agent-core:latest .

# Run Docker container
docker-run: docker-build
	@echo "Running Docker container..."
	docker run -p 4015:4015 agent-core:latest

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f agent-core
	rm -f *.db
	docker rmi agent-core:latest 2>/dev/null || true

# Run linters
lint:
	@echo "Running linters..."
	go fmt ./...
	go vet ./...

# Verify Docker build excludes test files
verify-no-tests:
	@echo "Running comprehensive Docker build verification..."
	@if [ -f scripts/verify-docker-build.sh ]; then \
		./scripts/verify-docker-build.sh; \
	else \
		echo "Building and verifying Docker image..."; \
		docker build -t agent-core:verify .; \
		echo "Image size: $$(docker images agent-core:verify --format '{{.Size}}')"; \
		docker rmi agent-core:verify; \
	fi

# Development build (includes tests)
build-dev:
	@echo "Building development version (with tests)..."
	go build -tags dev -o agent-core-dev main.go

# Run development server with optional PORT and CONFIG arguments
# Usage: make run [PORT=4016] [CONFIG=config-agent-b.json]
run:
	@if [ -n "$(PORT)" ] || [ -n "$(CONFIG)" ]; then \
		echo "Running development server..."; \
		[ -n "$(PORT)" ] && echo "  Port: $(PORT)" || echo "  Port: 4015 (default)"; \
		[ -n "$(CONFIG)" ] && echo "  Config: $(CONFIG)" || echo "  Config: agent-config.json (default)"; \
		PORT=$(PORT) CONFIG_PATH=$(CONFIG) go run .; \
	else \
		echo "Running development server on default port (4015)..."; \
		go run .; \
	fi

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

#==============================================================================
# A2A Multi-Agent
#==============================================================================

# Start two A2A peer agents (Ctrl+C stops both)
a2a:
	@./scripts/spawn-a2a-pair.sh start && \
	trap './scripts/spawn-a2a-pair.sh stop' EXIT INT TERM && \
	echo "Press Ctrl+C to stop both agents..." && \
	tail -f /tmp/a2a-test/agent-a.log /tmp/a2a-test/agent-b.log

# Stop both agents
a2a-stop:
	@./scripts/spawn-a2a-pair.sh stop

# Restart both agents
a2a-restart:
	@./scripts/spawn-a2a-pair.sh restart

# Check health and discovery
a2a-status:
	@./scripts/spawn-a2a-pair.sh status

# Run A2A protocol tests
a2a-test:
	@./scripts/spawn-a2a-pair.sh test

# Tail agent logs (use AGENT=a or AGENT=b for one agent)
a2a-logs:
	@./scripts/spawn-a2a-pair.sh logs $(AGENT)

# Run 3-agent collaboration + loop safety test
a2a-trio:
	@./scripts/a2a-trio-test.sh

# Quick test (skip slow tests)
test-quick:
	@echo "Running quick tests..."
	go test -short -v ./...

# Benchmark tests
benchmark:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

#==============================================================================
# Cross-Platform Release Builds (using Zig as C cross-compiler)
#==============================================================================
# NOTE: macOS builds require native compilation (macOS system frameworks
# CoreFoundation/Security are not available when cross-compiling from Linux).
# Use GitHub Actions or a macOS machine for darwin targets.

# Version and build info
VERSION := $(shell cat VERSION 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS_RELEASE := -ldflags="-w -s -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"
LDFLAGS_STATIC := -ldflags="-w -s -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -linkmode external -extldflags '-static'"
# Darwin builds: -s causes Go to pass -Wl,-x which Zig's macOS linker doesn't support
LDFLAGS_DARWIN := -ldflags="-w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Individual platform builds
release-linux-amd64:
	@echo "Building Linux AMD64..."
	@mkdir -p dist
	CGO_ENABLED=1 CC="zig cc -target x86_64-linux-musl" CXX="zig c++ -target x86_64-linux-musl" \
		GOOS=linux GOARCH=amd64 \
		go build $(LDFLAGS_STATIC) -o dist/agent-core-linux-amd64 .

release-linux-arm64:
	@echo "Building Linux ARM64..."
	@mkdir -p dist
	CGO_ENABLED=1 CC="zig cc -target aarch64-linux-musl" CXX="zig c++ -target aarch64-linux-musl" \
		GOOS=linux GOARCH=arm64 \
		go build $(LDFLAGS_STATIC) -o dist/agent-core-linux-arm64 .

release-darwin-amd64:
	@echo "Building macOS AMD64 (Intel)..."
	@mkdir -p dist
	CGO_ENABLED=1 CC="zig cc -target x86_64-macos" CXX="zig c++ -target x86_64-macos" \
		GOOS=darwin GOARCH=amd64 \
		go build $(LDFLAGS_DARWIN) -o dist/agent-core-darwin-amd64 .

release-darwin-arm64:
	@echo "Building macOS ARM64 (Apple Silicon)..."
	@mkdir -p dist
	CGO_ENABLED=1 CC="zig cc -target aarch64-macos" CXX="zig c++ -target aarch64-macos" \
		GOOS=darwin GOARCH=arm64 \
		go build $(LDFLAGS_DARWIN) -o dist/agent-core-darwin-arm64 .

release-windows-amd64:
	@echo "Building Windows AMD64..."
	@mkdir -p dist
	CGO_ENABLED=1 CC="zig cc -target x86_64-windows-gnu" CXX="zig c++ -target x86_64-windows-gnu" \
		GOOS=windows GOARCH=amd64 \
		go build $(LDFLAGS_RELEASE) -o dist/agent-core-windows-amd64.exe .

# Build platforms that work from Linux (Linux + Windows)
release: release-linux-amd64 release-linux-arm64 release-windows-amd64
	@echo ""
	@echo "Linux/Windows platforms built successfully!"
	@echo "(macOS builds require native compilation - run on macOS)"
	@ls -lh dist/

# Build all platforms (run on macOS or use CI)
release-all: release-linux-amd64 release-linux-arm64 release-darwin-amd64 release-darwin-arm64 release-windows-amd64
	@echo ""
	@echo "All platforms built successfully!"
	@ls -lh dist/

# Generate checksums
checksums:
	@echo "Generating checksums..."
	@cd dist && sha256sum agent-core-* > checksums.txt 2>/dev/null || shasum -a 256 agent-core-* > checksums.txt
	@cat dist/checksums.txt

# Create release archives
package: release checksums
	@echo "Creating release archives..."
	@cd dist && for f in agent-core-linux-* agent-core-darwin-*; do \
		[ -f "$$f" ] && [ ! -f "$$f.tar.gz" ] && tar -czvf "$$f.tar.gz" "$$f" && echo "Created $$f.tar.gz"; \
	done; true
	@cd dist && for f in agent-core-windows-*.exe; do \
		[ -f "$$f" ] && [ ! -f "$${f%.exe}.zip" ] && zip -q "$${f%.exe}.zip" "$$f" && echo "Created $${f%.exe}.zip"; \
	done; true

# Full distribution build
dist: package
	@echo ""
	@echo "=== Distribution Complete ==="
	@echo "Version: $(VERSION)"
	@echo ""
	@ls -lh dist/

# Clean distribution artifacts
clean-dist:
	@echo "Cleaning dist folder..."
	rm -rf dist/