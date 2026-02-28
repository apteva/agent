# Build stage
FROM golang:1.25-alpine AS builder

# Get version from VERSION file
ARG VERSION
COPY VERSION /tmp/VERSION
RUN VERSION=$(cat /tmp/VERSION 2>/dev/null || echo "1.0.0")

# Install build dependencies including gcc for CGO
RUN apk add --no-cache git gcc musl-dev sqlite-dev

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code (excluding test files via .dockerignore)
COPY . .

# Build the binary with static linking and optimizations
# -tags netgo ensures no CGO dependencies for networking
RUN VERSION=$(cat VERSION 2>/dev/null || echo "1.0.0") && \
    BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ') && \
    CGO_ENABLED=1 go build \
    -ldflags="-w -s -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -linkmode external -extldflags '-static'" \
    -tags netgo \
    -a \
    -o server .

# Final stage - scratch image (possible with static linking)
FROM scratch

# Add version labels
ARG VERSION
LABEL version=${VERSION}
LABEL description="AI Agent Server with MCP support"
LABEL maintainer="Agent Go"

# Set environment variables for data persistence
# DATA_DIR can be overridden at runtime with -e DATA_DIR=/custom/path
ENV DATA_DIR=/data

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary from builder
COPY --from=builder /build/server /server

# Copy VERSION file so runtime can read it
COPY --from=builder /build/VERSION /VERSION

# Expose port
EXPOSE 4015

# Define volume for data persistence
VOLUME ["/data"]

# Run the binary
ENTRYPOINT ["/server"]
