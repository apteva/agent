# Build Configuration

This document explains how the project is configured to exclude test files from Docker builds while maintaining them for development.

## Test Exclusion Strategy

### 1. Docker Ignore Configuration

The `.dockerignore` file excludes all test-related files from Docker builds:

```bash
# Exclude test files from Docker builds
*_test.go
test_utils.go
**/*_test.go
**/test_*
testdata/
tests/
```

### 2. Build Process

The `Dockerfile` uses a multi-stage build that:

- **Build Stage**: Compiles only the main binary without test dependencies
- **Final Stage**: Creates a minimal scratch-based image with just the binary

```dockerfile
# Explicitly build only main.go to exclude test files
RUN CGO_ENABLED=1 go build \
    -ldflags="-w -s -linkmode external -extldflags '-static'" \
    -tags netgo \
    -a \
    -o server main.go
```

### 3. File Structure

```
agent-core/
├── main.go                 # ✅ Included in Docker
├── config/                 # ✅ Included in Docker
├── providers/              # ✅ Included in Docker
├── stream/                 # ✅ Included in Docker
├── tools/                  # ✅ Included in Docker
├── pdf/                    # ✅ Included in Docker
├── *_test.go              # ❌ Excluded from Docker
├── test_utils.go          # ❌ Excluded from Docker
└── scripts/               # ❌ Excluded from Docker
```

## Build Commands

### Development (with tests)
```bash
# Run tests locally
make test

# Build development binary (includes all files)
make build-dev
```

### Production (without tests)
```bash
# Build optimized Docker image
make docker-build

# Verify test exclusion
make verify-no-tests

# Build and run container
make docker-run
```

## Verification

### Automatic Verification

Run the verification script to ensure test files are excluded:

```bash
make verify-no-tests
```

This script:
1. Builds the Docker image
2. Verifies the container starts correctly
3. Checks the image size (should be ~8MB without tests)
4. Confirms health endpoint responds

### Manual Verification

Check what files are in the Docker build context:

```bash
# See build context size
docker build --progress=plain -t agent-core:test . 2>&1 | grep "transferring context"

# Check final image size
docker images agent-core:test --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}"
```

Expected results:
- **Build context**: ~6KB (small, excludes test files)
- **Final image**: ~8MB (static binary + certificates)

## Benefits

### 🚀 **Performance**
- Smaller image size (8MB vs potentially 50MB+ with tests)
- Faster build and deployment times
- Reduced attack surface

### 🔒 **Security**
- No test utilities in production
- No test data or mock implementations
- Minimal dependencies

### 🏗️ **Maintainability**
- Clear separation between dev and prod builds
- Automated verification process
- Consistent build process

## Troubleshooting

### Build Fails
```bash
# Check what files are being copied
docker build --no-cache --progress=plain -t agent-core:debug . 2>&1 | grep COPY

# Verify .dockerignore patterns
docker build -t agent-core:debug . 2>&1 | head -20
```

### Large Image Size
If the image is unexpectedly large:

1. Check `.dockerignore` patterns
2. Verify test files aren't being copied
3. Run `make verify-no-tests` for detailed analysis

### Container Won't Start
```bash
# Check container logs
docker run agent-core:latest 2>&1

# Verify binary was built correctly
docker run --rm agent-core:latest /server --help
```

## CI/CD Integration

For automated builds, ensure:

```yaml
# Example GitHub Actions
- name: Build and verify Docker image
  run: |
    make docker-build
    make verify-no-tests
```

This ensures every build excludes test files and maintains production optimization.