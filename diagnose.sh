#!/bin/bash
set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  AWG-Easy Network Diagnostic${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if container is running
if ! docker ps --format '{{.Names}}' | grep -q '^awg-easy$'; then
    echo -e "${RED}✗ Container 'awg-easy' is not running!${NC}"
    echo ""
    echo "Start it with:"
    echo "  ./run.sh"
    echo "  or"
    echo "  docker-compose up -d"
    exit 1
fi

echo -e "${GREEN}✓ Container is running${NC}"
echo ""

# Check IP forwarding
echo -e "${BLUE}Checking IP forwarding...${NC}"
FORWARD=$(docker exec awg-easy sysctl -n net.ipv4.ip_forward 2>/dev/null || echo "0")
if [ "$FORWARD" = "1" ]; then
    echo -e "${GREEN}✓ IP forwarding is enabled${NC}"
else
    echo -e "${RED}✗ IP forwarding is DISABLED!${NC}"
    echo ""
    echo "This is the problem! Container needs to be recreated with:"
    echo "  --sysctl=net.ipv4.ip_forward=1"
    echo ""
    echo "Stop container and run ./run.sh again (it has the fix)"
    exit 1
fi
echo ""

# Check network interface
echo -e "${BLUE}Detecting network interface...${NC}"
INTERFACE=$(docker exec awg-easy ip route | grep default | awk '{print $5}')
if [ -z "$INTERFACE" ]; then
    echo -e "${RED}✗ Cannot detect network interface!${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Network interface: ${INTERFACE}${NC}"
echo ""

# Check WG_DEVICE setting
echo -e "${BLUE}Checking WG_DEVICE setting...${NC}"
WG_DEVICE=$(docker exec awg-easy printenv WG_DEVICE)
if [ "$WG_DEVICE" != "$INTERFACE" ]; then
    echo -e "${YELLOW}⚠ WG_DEVICE ($WG_DEVICE) differs from actual interface ($INTERFACE)${NC}"
    echo "This might cause routing issues!"
    echo ""
else
    echo -e "${GREEN}✓ WG_DEVICE matches: ${WG_DEVICE}${NC}"
fi
echo ""

# Check WireGuard interface
echo -e "${BLUE}Checking WireGuard interface...${NC}"
if docker exec awg-easy wg show wg0 >/dev/null 2>&1; then
    echo -e "${GREEN}✓ WireGuard interface wg0 is up${NC}"
    
    # Check for peers
    PEER_COUNT=$(docker exec awg-easy wg show wg0 peers | wc -l)
    echo "  Peers configured: $PEER_COUNT"
    
    if [ "$PEER_COUNT" -gt 0 ]; then
        # Check handshakes
        docker exec awg-easy wg show wg0 latest-handshakes | while read peer timestamp; do
            if [ "$timestamp" -gt 0 ]; then
                AGE=$(($(date +%s) - timestamp))
                if [ "$AGE" -lt 180 ]; then
                    echo -e "  ${GREEN}✓ Peer has recent handshake (${AGE}s ago)${NC}"
                else
                    echo -e "  ${YELLOW}⚠ Peer handshake is old (${AGE}s ago)${NC}"
                fi
            else
                echo -e "  ${RED}✗ Peer has never connected${NC}"
            fi
        done
    fi
else
    echo -e "${RED}✗ WireGuard interface is DOWN!${NC}"
    echo ""
    echo "Try restarting WireGuard:"
    echo "  docker exec awg-easy wg-quick down wg0"
    echo "  docker exec awg-easy wg-quick up wg0"
    exit 1
fi
echo ""

# Check NAT (MASQUERADE)
echo -e "${BLUE}Checking NAT (MASQUERADE) rules...${NC}"
if docker exec awg-easy iptables -t nat -L POSTROUTING -v -n 2>/dev/null | grep -q MASQUERADE; then
    echo -e "${GREEN}✓ MASQUERADE rule exists${NC}"
    docker exec awg-easy iptables -t nat -L POSTROUTING -v -n 2>/dev/null | grep MASQUERADE | head -1
else
    echo -e "${RED}✗ MASQUERADE rule NOT found!${NC}"
    echo ""
    echo "Adding MASQUERADE rule..."
    docker exec awg-easy iptables -t nat -A POSTROUTING -s 10.8.0.0/24 -o "$INTERFACE" -j MASQUERADE
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ MASQUERADE rule added${NC}"
    else
        echo -e "${RED}✗ Failed to add MASQUERADE rule${NC}"
        exit 1
    fi
fi
echo ""

# Check FORWARD rules
echo -e "${BLUE}Checking FORWARD rules...${NC}"
if docker exec awg-easy iptables -L FORWARD -v -n 2>/dev/null | grep -q "wg0"; then
    echo -e "${GREEN}✓ FORWARD rules exist${NC}"
    docker exec awg-easy iptables -L FORWARD -v -n 2>/dev/null | grep wg0
else
    echo -e "${YELLOW}⚠ FORWARD rules not found, adding...${NC}"
    docker exec awg-easy iptables -A FORWARD -i wg0 -j ACCEPT
    docker exec awg-easy iptables -A FORWARD -o wg0 -j ACCEPT
    echo -e "${GREEN}✓ FORWARD rules added${NC}"
fi
echo ""

# Summary
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Summary${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo "Network interface:  $INTERFACE"
echo "WG_DEVICE setting:  $WG_DEVICE"
echo "IP forwarding:      $FORWARD"
echo "WireGuard status:   UP"
echo "Peers:              $PEER_COUNT"
echo ""

# Test suggestions
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Test from client:${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo "1. Ping VPN server:"
echo "   ping 10.8.0.1"
echo ""
echo "2. Ping external IP:"
echo "   ping 8.8.8.8"
echo ""
echo "3. Ping DNS name:"
echo "   ping google.com"
echo ""
echo "4. Check your IP:"
echo "   curl ifconfig.me"
echo "   (should show server IP, not your real IP)"
echo ""

# Check if any fixes were applied
if docker exec awg-easy iptables -t nat -L POSTROUTING -v -n 2>/dev/null | grep -q MASQUERADE && \
   docker exec awg-easy iptables -L FORWARD -v -n 2>/dev/null | grep -q wg0; then
    echo -e "${GREEN}✓ All checks passed! Try connecting your client now.${NC}"
else
    echo -e "${YELLOW}⚠ Some issues detected. Review the output above.${NC}"
fi
