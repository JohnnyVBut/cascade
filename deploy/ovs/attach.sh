#!/usr/bin/env bash
# =============================================================================
# Cascade — OVS interface attach helper
#
# Attaches an Open vSwitch port to a running Cascade container started with
# network_mode: none, assigns an IP address and default gateway.
#
# Usage:
#   bash deploy/ovs/attach.sh [OPTIONS]
#
# Options (interactive if not specified):
#   --container NAME   Container name (default: cascade)
#   --bridge    NAME   OVS bridge name (default: br-trunk)
#   --iface     NAME   Interface name inside the container (default: eth0)
#   --ip        CIDR   IP address with prefix, e.g. 192.168.20.5/24
#   --gateway   IP     Default gateway IP, e.g. 192.168.20.1
#   --vlan      ID     VLAN ID for 802.1q tagging (optional)
#
# Example (VLAN 20, explicit args):
#   bash deploy/ovs/attach.sh \
#     --bridge br-trunk --ip 192.168.20.5/24 --gateway 192.168.20.1 --vlan 20
#
# Example (read from deploy/.env):
#   bash deploy/ovs/attach.sh  # interactive prompts for missing values
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/../.env"

G='\033[0;32m'; Y='\033[1;33m'; R='\033[0;31m'; B='\033[0;34m'; N='\033[0m'
ok()   { echo -e "${G}  [✓]${N} $*"; }
info() { echo -e "${B}  [→]${N} $*"; }
warn() { echo -e "${Y}  [!]${N} $*"; }
fail() { echo -e "${R}  [✗]${N} $*"; exit 1; }

# ── Defaults (overridable from .env or CLI args) ──────────────────────────────
CONTAINER=${CASCADE_CONTAINER:-cascade}
OVS_BRIDGE=${OVS_BRIDGE:-br-trunk}
CONTAINER_IFACE=${OVS_IFACE:-eth0}
CONTAINER_IP=${OVS_IP:-}
GATEWAY=${OVS_GATEWAY:-}
VLAN=${OVS_VLAN:-}

# ── Load .env if present ──────────────────────────────────────────────────────
[[ -f "$ENV_FILE" ]] && source "$ENV_FILE"

# ── Parse CLI args ────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --container) CONTAINER="$2";       shift 2 ;;
    --bridge)    OVS_BRIDGE="$2";      shift 2 ;;
    --iface)     CONTAINER_IFACE="$2"; shift 2 ;;
    --ip)        CONTAINER_IP="$2";    shift 2 ;;
    --gateway)   GATEWAY="$2";         shift 2 ;;
    --vlan)      VLAN="$2";            shift 2 ;;
    *) fail "Unknown argument: $1" ;;
  esac
done

# ── Interactive prompts for missing values ────────────────────────────────────
ask() {
  local var="$1" prompt="$2"
  [[ -n "${!var:-}" ]] && { ok "$prompt: ${!var}"; return; }
  read -rp "  $prompt: " "$var"
}

echo ""
echo -e "${B}========================================${N}"
echo -e "${B}  Cascade — OVS Interface Attach${N}"
echo -e "${B}========================================${N}"
echo ""

ask CONTAINER      "Container name"
ask OVS_BRIDGE     "OVS bridge"
ask CONTAINER_IFACE "Interface name inside container"
ask CONTAINER_IP   "Container IP/prefix (e.g. 192.168.20.5/24)"
ask GATEWAY        "Default gateway"
# VLAN is optional — skip prompt if empty
if [[ -z "$VLAN" ]]; then
  read -rp "  VLAN ID (leave empty for untagged): " VLAN
fi

# ── Validate ──────────────────────────────────────────────────────────────────
command -v ovs-docker &>/dev/null || fail "ovs-docker not found. Install openvswitch-switch."
docker inspect "$CONTAINER" &>/dev/null   || fail "Container '$CONTAINER' not found or not running."

# ── Add port (without --vlan: not supported in all ovs-docker versions) ───────
CMD="ovs-docker add-port $OVS_BRIDGE $CONTAINER_IFACE $CONTAINER \
  --ipaddress=$CONTAINER_IP \
  --gateway=$GATEWAY"

info "Running: $CMD"
eval $CMD

# ── Set VLAN tag via ovs-vsctl (the portable way) ─────────────────────────────
# ovs-docker naming convention: {first 13 chars of container ID}_l
# Some versions also set external_ids — try both.
if [[ -n "$VLAN" ]]; then
  CONTAINER_ID=$(docker inspect --format='{{.Id}}' "$CONTAINER")

  # Method 1: external_ids (newer ovs-docker versions)
  PORT_NAME=$(ovs-vsctl --data=bare --no-heading --columns=name find interface \
    external_ids:container_id="$CONTAINER_ID" \
    external_ids:container_iface="$CONTAINER_IFACE" 2>/dev/null || true)

  # Method 2: standard naming pattern {13-char-id}_l (most versions)
  if [[ -z "$PORT_NAME" ]]; then
    PORT_NAME="${CONTAINER_ID:0:13}_l"
    # Verify it actually exists on the bridge
    if ! ovs-vsctl list-ports "$OVS_BRIDGE" 2>/dev/null | grep -qx "$PORT_NAME"; then
      PORT_NAME=""
    fi
  fi

  if [[ -n "$PORT_NAME" ]]; then
    ovs-vsctl set port "$PORT_NAME" tag="$VLAN"
    ok "VLAN tag $VLAN set on port $PORT_NAME"
  else
    warn "Could not find OVS port to set VLAN tag — set manually:"
    warn "  ovs-vsctl set port <port-name> tag=$VLAN"
    warn "  (list ports: ovs-vsctl list-ports $OVS_BRIDGE)"
  fi
fi

ok "Interface attached:"
ok "  Container : $CONTAINER"
ok "  Bridge    : $OVS_BRIDGE"
ok "  Interface : $CONTAINER_IFACE"
ok "  IP        : $CONTAINER_IP"
ok "  Gateway   : $GATEWAY"
[[ -n "$VLAN" ]] && ok "  VLAN      : $VLAN"
echo ""

# ── Verify container sees the route ──────────────────────────────────────────
sleep 1
ROUTE=$(docker exec "$CONTAINER" ip route show default 2>/dev/null || echo "(not yet)")
info "Container default route: $ROUTE"

echo ""
ok "Done. Cascade should start within a few seconds."
info "Watch logs: docker logs -f $CONTAINER"
