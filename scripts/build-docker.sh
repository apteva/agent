#!/bin/bash

# Build Docker/Podman image with automatic version tagging
# Usage: ./scripts/build-docker.sh [--push] [--latest]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Auto-detect container runtime (prefer docker, fallback to podman)
if command -v docker &> /dev/null; then
    CONTAINER_CMD="docker"
elif command -v podman &> /dev/null; then
    CONTAINER_CMD="podman"
else
    echo -e "${RED}Error: Neither docker nor podman found${NC}"
    exit 1
fi

echo -e "${BLUE}Using container runtime: ${GREEN}$CONTAINER_CMD${NC}"

# Default values
PUSH=false
TAG_LATEST=true
IMAGE_NAME="agent-core"
REGISTRY=""

# Registry presets
REGISTRY_LOCAL="localhost:5000"
REGISTRY_PRODUCTION="registry.omnikit.co"
REGISTRY_AGENTS="159.69.221.243:5000"

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --push)
            PUSH=true
            shift
            ;;
        --no-latest)
            TAG_LATEST=false
            shift
            ;;
        --image-name)
            IMAGE_NAME="$2"
            shift 2
            ;;
        --registry)
            REGISTRY="$2"
            PUSH=true
            shift 2
            ;;
        --local)
            REGISTRY="$REGISTRY_LOCAL"
            PUSH=true
            shift
            ;;
        --production)
            REGISTRY="$REGISTRY_PRODUCTION"
            PUSH=true
            shift
            ;;
        --agents)
            REGISTRY="$REGISTRY_AGENTS"
            PUSH=true
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --push         Push the image to registry after building"
            echo "  --no-latest    Skip tagging as 'latest' (latest is created by default)"
            echo "  --image-name   Set custom image name (default: agent-core)"
            echo "  --registry     Push to custom registry URL"
            echo ""
            echo "Registry shortcuts:"
            echo "  --local        Push to local registry ($REGISTRY_LOCAL)"
            echo "  --production   Push to production registry ($REGISTRY_PRODUCTION)"
            echo "  --agents       Push to agents server registry ($REGISTRY_AGENTS)"
            exit 0
            ;;
        *)
            echo "Unknown option $1"
            exit 1
            ;;
    esac
done

# Check if VERSION file exists
if [[ ! -f "VERSION" ]]; then
    echo -e "${RED}Error: VERSION file not found${NC}"
    exit 1
fi

# Read version from VERSION file
VERSION=$(cat VERSION | tr -d '\n\r')

if [[ -z "$VERSION" ]]; then
    echo -e "${RED}Error: VERSION file is empty${NC}"
    exit 1
fi

echo -e "${BLUE}Building Docker image for version: ${GREEN}$VERSION${NC}"

# Build the container image with version as build arg
echo -e "${YELLOW}Building image...${NC}"
$CONTAINER_CMD build \
    --build-arg VERSION="$VERSION" \
    -t "$IMAGE_NAME:$VERSION" \
    .

# Tag as latest (default behavior)
if [[ "$TAG_LATEST" == true ]]; then
    echo -e "${YELLOW}Tagging as latest...${NC}"
    $CONTAINER_CMD tag "$IMAGE_NAME:$VERSION" "$IMAGE_NAME:latest"
fi

echo -e "${GREEN}✅ Successfully built Docker image${NC}"
echo -e "${BLUE}Tags created:${NC}"
echo -e "  - $IMAGE_NAME:$VERSION"

if [[ "$TAG_LATEST" == true ]]; then
    echo -e "  - $IMAGE_NAME:latest"
fi

# Push to registry if requested
if [[ "$PUSH" == true ]]; then
    if [[ -n "$REGISTRY" ]]; then
        # Push to custom registry
        REGISTRY_IMAGE="$REGISTRY/$IMAGE_NAME"
        echo -e "${YELLOW}Tagging for registry: ${GREEN}$REGISTRY${NC}"

        $CONTAINER_CMD tag "$IMAGE_NAME:$VERSION" "$REGISTRY_IMAGE:$VERSION"
        echo -e "  - Tagged: $REGISTRY_IMAGE:$VERSION"

        if [[ "$TAG_LATEST" == true ]]; then
            $CONTAINER_CMD tag "$IMAGE_NAME:$VERSION" "$REGISTRY_IMAGE:latest"
            echo -e "  - Tagged: $REGISTRY_IMAGE:latest"
        fi

        echo -e "${YELLOW}Pushing to registry...${NC}"
        $CONTAINER_CMD push "$REGISTRY_IMAGE:$VERSION"

        if [[ "$TAG_LATEST" == true ]]; then
            $CONTAINER_CMD push "$REGISTRY_IMAGE:latest"
        fi

        echo -e "${GREEN}✅ Successfully pushed to $REGISTRY${NC}"
    else
        # Push to default registry (Docker Hub or configured default)
        echo -e "${YELLOW}Pushing to default registry...${NC}"
        $CONTAINER_CMD push "$IMAGE_NAME:$VERSION"

        if [[ "$TAG_LATEST" == true ]]; then
            $CONTAINER_CMD push "$IMAGE_NAME:latest"
        fi

        echo -e "${GREEN}✅ Successfully pushed to registry${NC}"
    fi
fi

# Show image size
echo -e "${BLUE}Image size:${NC}"
$CONTAINER_CMD images "$IMAGE_NAME:$VERSION" --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}"

echo -e "${GREEN}Build complete!${NC}"
echo -e "${BLUE}To run the container:${NC}"
echo -e "  $CONTAINER_CMD run -p 4015:4015 $IMAGE_NAME:$VERSION"