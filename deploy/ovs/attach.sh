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

# ── Load .env first so its values are available for defaults below ────────────
[[ -f "$ENV_FILE" ]] && source "$ENV_FILE"

# ── Defaults (overridable from .env or CLI args) ──────────────────────────────
CONTAINER=${CASCADE_CONTAINER:-cascade}
OVS_BRIDGE=${OVS_BRIDGE:-br-trunk}
CONTAINER_IFACE=${OVS_IFACE:-eth0}
CONTAINER_IP=${OVS_IP:-}
GATEWAY=${OVS_GATEWAY:-}
VLAN=${OVS_VLAN:-}

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

# ── Remove stale port if present (container restart changes the veth pair) ────
if ovs-docker del-port "$OVS_BRIDGE" "$CONTAINER_IFACE" "$CONTAINER" 2>/dev/null; then
  info "Removed stale OVS port (container was restarted)"
  sleep 0.5
fi

# ── Add port (without --vlan: not supported in all ovs-docker versions) ───────
CMD="ovs-docker add-port $OVS_BRIDGE $CONTAINER_IFACE $CONTAINER \
  --ipaddress=$CONTAINER_IP \
  --gateway=$GATEWAY"

info "Running: $CMD"
eval $CMD

# ── Set VLAN tag via ovs-vsctl ────────────────────────────────────────────────
# Finding the OVS port reliably:
#   The iflink file inside the container holds the ifindex of the host-side
#   veth peer. We resolve that to an interface name, then look it up in OVS.
#   This works regardless of container ID, ovs-docker version, or naming scheme.
if [[ -n "$VLAN" ]]; then
  PORT_NAME=""

  # Method 1: iflink → host veth name → OVS port (most reliable)
  PEER_IDX=$(docker exec "$CONTAINER" cat /sys/class/net/"$CONTAINER_IFACE"/iflink 2>/dev/null || true)
  if [[ -n "$PEER_IDX" ]]; then
    HOST_VETH=$(ip link show | awk -F': ' "/^${PEER_IDX}:/{print \$2}" | tr -d ' @' | head -1)
    if [[ -n "$HOST_VETH" ]]; then
      PORT_NAME=$(ovs-vsctl --data=bare --no-heading --columns=name \
        find interface name="$HOST_VETH" 2>/dev/null || true)
    fi
  fi

  # Method 2: external_ids set by newer ovs-docker versions
  if [[ -z "$PORT_NAME" ]]; then
    CONTAINER_ID=$(docker inspect --format='{{.Id}}' "$CONTAINER")
    PORT_NAME=$(ovs-vsctl --data=bare --no-heading --columns=name find interface \
      external_ids:container_id="$CONTAINER_ID" \
      external_ids:container_iface="$CONTAINER_IFACE" 2>/dev/null || true)
  fi

  # Method 3: {first 13 chars of container ID}_l naming pattern
  if [[ -z "$PORT_NAME" ]]; then
    CONTAINER_ID=$(docker inspect --format='{{.Id}}' "$CONTAINER" 2>/dev/null || true)
    CANDIDATE="${CONTAINER_ID:0:13}_l"
    if ovs-vsctl list-ports "$OVS_BRIDGE" 2>/dev/null | grep -qx "$CANDIDATE"; then
      PORT_NAME="$CANDIDATE"
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
