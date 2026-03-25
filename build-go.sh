#!/bin/bash
set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Building Cascade (Go/Fiber)${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

cd "$(dirname "$0")"

# Verify we're not on master (local builds are for development only)
BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [[ "$BRANCH" == "master" ]]; then
    echo -e "${YELLOW}Note: on master branch. Production deploys use the GHCR image:${NC}"
    echo -e "  docker compose -f docker-compose.go.yml pull"
    echo -e "  docker compose -f docker-compose.go.yml up -d"
    echo -e "${YELLOW}Building locally anyway (e.g. for testing before push)...${NC}"
fi

echo -e "${GREEN}Building Docker image (Go/Fiber)...${NC}"
docker build --network=host -f Dockerfile.go -t cascade:latest .

# Detect compose command (v2 plugin vs v1 standalone)
if docker compose version &>/dev/null 2>&1; then
    COMPOSE="docker compose"
elif command -v docker-compose &>/dev/null; then
    COMPOSE="docker-compose"
else
    COMPOSE="docker compose"  # best guess, will show a clear error if missing
fi

echo ""
echo -e "${GREEN}✓ Build complete!${NC}"
echo ""
echo "Image tag: cascade:latest"
echo ""
echo "Next steps (local dev with locally-built image):"
echo "  ${COMPOSE} -f docker-compose.go.yml -f docker-compose.override.yml down"
echo "  ${COMPOSE} -f docker-compose.go.yml -f docker-compose.override.yml up -d"
echo ""
echo "  docker logs cascade"
echo "  curl http://127.0.0.1:8888/api/health"
echo ""
echo -e "${YELLOW}Production deploy (uses GHCR image built by CI):${NC}"
echo "  ${COMPOSE} -f docker-compose.go.yml pull"
echo "  ${COMPOSE} -f docker-compose.go.yml up -d"
