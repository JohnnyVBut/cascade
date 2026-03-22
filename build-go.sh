#!/bin/bash
set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Building AWG-Easy 3.0 (Go/Fiber)${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

cd "$(dirname "$0")"

# Verify we're on the right branch
BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [[ "$BRANCH" != "feature/go-rewrite" ]]; then
    echo -e "${YELLOW}Warning: not on feature/go-rewrite (current: $BRANCH)${NC}"
fi

echo -e "${GREEN}Building Docker image (Go/Fiber)...${NC}"
docker build --network=host -f Dockerfile.go -t awg2-easy-go:latest .

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
echo "Image tag: awg2-easy-go:latest"
echo ""
echo "Next steps:"
echo "  1. Edit docker-compose.go.yml with your settings (WG_HOST, PASSWORD_HASH)"
echo "  2. Deploy:"
echo "     ${COMPOSE} -f docker-compose.go.yml down && ${COMPOSE} -f docker-compose.go.yml up -d"
echo "  3. Check logs:"
echo "     docker logs awg-router"
echo "  4. Healthcheck:"
echo "     curl http://127.0.0.1:8888/api/health"
