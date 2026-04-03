#!/usr/bin/env bash
# =============================================================================
# Cascade — OVS interface attach helper
#
# First run: interactive prompts for all missing values, saves to deploy/.env.
# Subsequent runs: reads everything from deploy/.env, no prompts.
# Also starts the container if it is not already running.
#
# Usage:
#   bash deploy/ovs/attach.sh [OPTIONS]
#
# Options (override .env / prompts):
#   --container NAME   Container name (default: cascade)
#   --bridge    NAME   OVS bridge name (default: br-trunk)
#   --iface     NAME   Interface name inside the container (default: eth0)
#   --ip        CIDR   IP address with prefix, e.g. 192.168.20.5/24
#   --gateway   IP     Default gateway IP, e.g. 192.168.20.1
#   --vlan      ID     VLAN ID for 802.1q tagging (optional)
#   --mac       MAC    Fixed MAC address (auto-derived from IP if omitted)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/../.env"
COMPOSE_FILE="$SCRIPT_DIR/../../docker-compose.isolated.yml"

G='\033[0;32m'; Y='\033[1;33m'; R='\033[0;31m'; B='\033[0;34m'; N='\033[0m'
ok()   { echo -e "${G}  [✓]${N} $*"; }
info() { echo -e "${B}  [→]${N} $*"; }
warn() { echo -e "${Y}  [!]${N} $*"; }
fail() { echo -e "${R}  [✗]${N} $*"; exit 1; }

# ── Load .env first so its values are available for defaults below ────────────
[[ -f "$ENV_FILE" ]] && source "$ENV_FILE"

# ── Defaults (overridable from .env or CLI args) ──────────────────────────────
CONTAINER=${CASCADE_CONTAINER:-cascade}
OVS_BRIDGE=${OVS_BRIDGE:-}
CONTAINER_IFACE=${OVS_IFACE:-eth0}
CONTAINER_IP=${OVS_IP:-}
GATEWAY=${OVS_GATEWAY:-}
VLAN=${OVS_VLAN:-}
MAC_ADDR=${OVS_MAC:-}

# ── Parse CLI args ────────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --container) CONTAINER="$2";       shift 2 ;;
    --bridge)    OVS_BRIDGE="$2";      shift 2 ;;
    --iface)     CONTAINER_IFACE="$2"; shift 2 ;;
    --ip)        CONTAINER_IP="$2";    shift 2 ;;
    --gateway)   GATEWAY="$2";         shift 2 ;;
    --vlan)      VLAN="$2";            shift 2 ;;
    --mac)       MAC_ADDR="$2";        shift 2 ;;
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

ask OVS_BRIDGE      "OVS bridge"
ask CONTAINER_IP    "Container IP/prefix (e.g. 10.72.20.40/24)"
ask GATEWAY         "Default gateway"
if [[ -z "$VLAN" ]]; then
  read -rp "  VLAN ID (leave empty for untagged): " VLAN
fi

# ── Save all collected values to .env for future runs ─────────────────────────
# WG_HOST is intentionally NOT saved — Cascade will auto-detect the public IP
# via external services (ip.sb, ifconfig.me, etc.). Saving the private OVS IP
# would override auto-detect and break client endpoint generation behind NAT.
save_var() {
  local key="$1" value="$2"
  [[ -z "$value" ]] && return
  touch "$ENV_FILE"
  if grep -q "^${key}=" "$ENV_FILE" 2>/dev/null; then
    sed -i "s|^${key}=.*|${key}=${value}|" "$ENV_FILE"
  else
    echo "${key}=${value}" >> "$ENV_FILE"
  fi
}

save_var OVS_BRIDGE  "$OVS_BRIDGE"
save_var OVS_IP      "$CONTAINER_IP"
save_var OVS_GATEWAY "$GATEWAY"
save_var OVS_VLAN    "$VLAN"
save_var OVS_IFACE   "$CONTAINER_IFACE"
ok "Config saved to $ENV_FILE"
info "WG_HOST not saved — public IP will be auto-detected by Cascade"

# Remove WG_HOST from .env if it was previously saved as a private IP
# (old attach.sh versions did this; keep it only if user manually set a public address)
if grep -q "^WG_HOST=" "$ENV_FILE" 2>/dev/null; then
  SAVED_HOST=$(grep "^WG_HOST=" "$ENV_FILE" | cut -d= -f2)
  if [[ "$SAVED_HOST" =~ ^(10\.|172\.(1[6-9]|2[0-9]|3[01])\.|192\.168\.|127\.|169\.254\.) ]]; then
    sed -i "/^WG_HOST=/d" "$ENV_FILE"
    info "Removed private WG_HOST=$SAVED_HOST from $ENV_FILE"
  fi
fi

# ── Validate tools ────────────────────────────────────────────────────────────
command -v ovs-docker &>/dev/null || fail "ovs-docker not found. Install openvswitch-switch."

# ── Start container if not running ────────────────────────────────────────────
# Use docker ps (not docker inspect) — inspect also matches images by name,
# which have no .State and cause ovs-docker to fail with "map has no entry for key State".
is_running() { docker ps --filter "name=^/${CONTAINER}$" --filter "status=running" -q | grep -q .; }

if ! is_running; then
  if [[ ! -f "$COMPOSE_FILE" ]]; then
    fail "Container '$CONTAINER' not running and $COMPOSE_FILE not found."
  fi
  info "Container not running — starting with docker compose..."
  docker compose -f "$COMPOSE_FILE" up -d
  info "Waiting for container to be ready..."
  for i in $(seq 1 30); do
    is_running && break
    sleep 1
    [[ $i -eq 30 ]] && fail "Container '$CONTAINER' did not start after 30s — check: docker logs $CONTAINER"
  done
fi

# ── Remove stale ports (container restart creates new veth, old ports accumulate) ──
if ovs-docker del-port "$OVS_BRIDGE" "$CONTAINER_IFACE" "$CONTAINER" 2>/dev/null; then
  info "Removed stale OVS port via ovs-docker"
fi
# Purge dead ports (netdev missing) to keep OVS clean
DEAD_PORTS=$(ovs-vsctl list interface 2>/dev/null \
  | awk '/error.*No such device/{found=1} found && /name.*:/{print; found=0}' \
  | grep -oP '(?<=name\s{16}: ")[^"]+' || true)
for dp in $DEAD_PORTS; do
  ovs-vsctl --if-exists del-port "$OVS_BRIDGE" "$dp" 2>/dev/null && info "Purged dead OVS port: $dp"
done
sleep 0.5

# ── Add port ──────────────────────────────────────────────────────────────────
CMD="ovs-docker add-port $OVS_BRIDGE $CONTAINER_IFACE $CONTAINER \
  --ipaddress=$CONTAINER_IP \
  --gateway=$GATEWAY"
info "Running: $CMD"
eval $CMD

# ── Set fixed MAC address (prevents ARP churn on container restart) ───────────
# Derive deterministically from IP if not set: 02:00:<o1>:<o2>:<o3>:<o4>
if [[ -z "$MAC_ADDR" ]]; then
  IFS='.' read -r _o1 _o2 _o3 _o4 <<< "${CONTAINER_IP%%/*}"
  MAC_ADDR=$(printf "02:00:%02x:%02x:%02x:%02x" "$_o1" "$_o2" "$_o3" "$_o4")
  info "Generated MAC from IP: $MAC_ADDR"
fi
docker exec "$CONTAINER" ip link set "$CONTAINER_IFACE" down
docker exec "$CONTAINER" ip link set "$CONTAINER_IFACE" address "$MAC_ADDR"
docker exec "$CONTAINER" ip link set "$CONTAINER_IFACE" up
# ip link set down removes the default route — re-add it
docker exec "$CONTAINER" ip route add default via "$GATEWAY" dev "$CONTAINER_IFACE" 2>/dev/null || true
# Gratuitous ARP to flush upstream ARP caches immediately
docker exec "$CONTAINER" arping -c 1 -A -I "$CONTAINER_IFACE" "${CONTAINER_IP%%/*}" 2>/dev/null || true
ok "MAC address set to $MAC_ADDR"

# ── Set VLAN tag via ovs-vsctl ────────────────────────────────────────────────
if [[ -n "$VLAN" ]]; then
  PORT_NAME=""

  # Method 1: iflink → host veth name → OVS port (most reliable)
  PEER_IDX=$(docker exec "$CONTAINER" cat /sys/class/net/"$CONTAINER_IFACE"/iflink 2>/dev/null || true)
  if [[ -n "$PEER_IDX" ]]; then
    HOST_VETH=$(ip link show | awk -F': ' "/^${PEER_IDX}:/{print \$2}" | cut -d@ -f1 | head -1)
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
[[ -n "$VLAN" ]]     && ok "  VLAN      : $VLAN"
[[ -n "$MAC_ADDR" ]] && ok "  MAC       : $MAC_ADDR"
echo ""

# ── Verify container sees the route ──────────────────────────────────────────
sleep 1
ROUTE=$(docker exec "$CONTAINER" ip route show default 2>/dev/null || echo "(not yet)")
info "Container default route: $ROUTE"

echo ""
ok "Done. Cascade should start within a few seconds."
info "Watch logs: docker logs -f $CONTAINER"
