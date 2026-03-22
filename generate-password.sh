#!/bin/bash
set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  AWG-Easy Password Hash Generator${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if image exists
if ! docker image inspect awg-easy:2.0 >/dev/null 2>&1; then
    echo -e "${YELLOW}Docker image 'awg-easy:2.0' not found.${NC}"
    echo "Please run './build.sh' first."
    exit 1
fi

# Get password
read -sp "Enter Web UI password: " PASSWORD
echo ""

if [ -z "$PASSWORD" ]; then
    echo "Password cannot be empty!"
    exit 1
fi

# Generate hash
echo ""
echo -e "${BLUE}Generating bcrypt hash...${NC}"
WGPW_OUTPUT=$(docker run --rm awg-easy:2.0 wgpw "$PASSWORD")

# Extract hash from output: PASSWORD_HASH='$2y$...'
HASH=$(echo "$WGPW_OUTPUT" | grep -oP "PASSWORD_HASH='\K[^']+")

if [ -z "$HASH" ]; then
    echo -e "${RED}Failed to generate password hash!${NC}"
    echo "Output was: $WGPW_OUTPUT"
    exit 1
fi

echo ""
echo -e "${GREEN}✓ Password hash generated!${NC}"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Copy this hash to your docker-compose.yml:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo -e "${YELLOW}PASSWORD_HASH=${HASH}${NC}"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo -e "${BLUE}For docker-compose.yml:${NC}"
echo "  Replace $ with \$\$ (double dollar sign)"
echo ""
echo "Example:"
# Escape $ for display
ESCAPED_HASH=$(echo "$HASH" | sed 's/\$/\$\$/g')
echo "  - PASSWORD_HASH=${ESCAPED_HASH}"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo -e "${BLUE}For docker run command:${NC}"
echo "  Use single quotes:"
echo ""
echo "  -e PASSWORD_HASH='${HASH}' \\"
echo ""
