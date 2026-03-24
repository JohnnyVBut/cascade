#!/usr/bin/env bash
# =============================================================================
# Cascade — Switch AmneziaWG run mode without full re-setup
# Usage:
#   bash deploy/switch-mode.sh --userspace   # switch to amneziawg-go
#   bash deploy/switch-mode.sh --kernel      # switch to kernel module
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="$SCRIPT_DIR/.env"
COMPOSE_FILE="$REPO_DIR/docker-compose.go.yml"

G='\033[0;32m'; Y='\033[1;33m'; R='\033[0;31m'; B='\033[0;34m'; N='\033[0m'
ok()   { echo -e "  ${G}✓${N} $*"; }
info() { echo -e "  ${B}→${N} $*"; }
warn() { echo -e "  ${Y}⚠${N} $*"; }
fail() { echo -e "  ${R}✗${N} $*"; exit 1; }

# ── Parse args ────────────────────────────────────────────────────────────────
MODE=""
for arg in "$@"; do
  case "$arg" in
    --userspace) MODE="userspace" ;;
    --kernel)    MODE="kernel"    ;;
    -h|--help)
      echo "Usage: bash deploy/switch-mode.sh --userspace | --kernel"
      echo ""
      echo "  --userspace   Use amneziawg-go (stable, no kernel module required)"
      echo "  --kernel      Use AmneziaWG kernel module (faster, may have deadlock issues)"
      exit 0
      ;;
    *) fail "Unknown argument: $arg. Use --userspace or --kernel" ;;
  esac
done

[[ -z "$MODE" ]] && { echo "Usage: bash deploy/switch-mode.sh --userspace | --kernel"; exit 1; }

# ── Must be root ──────────────────────────────────────────────────────────────
[[ "$(id -u)" -ne 0 ]] && fail "Run as root: sudo bash deploy/switch-mode.sh --$MODE"

# ── Detect docker compose ─────────────────────────────────────────────────────
if docker compose version &>/dev/null 2>&1; then
  COMPOSE_CMD="docker compose"
elif command -v docker-compose &>/dev/null; then
  COMPOSE_CMD="docker-compose"
else
  fail "docker compose not found"
fi

# ── Mode helpers ──────────────────────────────────────────────────────────────
apply_userspace() {
  info "Switching to userspace mode (amneziawg-go)..."
  if lsmod | grep -q amneziawg 2>/dev/null; then
    info "Unloading amneziawg kernel module..."
    modprobe -r amneziawg 2>/dev/null || warn "Could not unload — reboot may be required"
  fi
  echo "blacklist amneziawg" > /etc/modprobe.d/amneziawg-blacklist.conf
  rm -f /etc/modules-load.d/amneziawg.conf
  ok "Kernel module blacklisted"
}

apply_kernel() {
  info "Switching to kernel module mode..."
  rm -f /etc/modprobe.d/amneziawg-blacklist.conf
  if lsmod | grep -q amneziawg 2>/dev/null; then
    ok "amneziawg already loaded"
  elif dpkg -l amneziawg &>/dev/null 2>&1; then
    modprobe amneziawg
    echo "amneziawg" > /etc/modules-load.d/amneziawg.conf
    ok "amneziawg loaded"
  else
    info "Installing amneziawg kernel module (ppa:amnezia/ppa)..."
    add-apt-repository -y ppa:amnezia/ppa > /dev/null 2>&1
    apt-get update -qq
    apt-get install -y amneziawg
    modprobe amneziawg
    echo "amneziawg" > /etc/modules-load.d/amneziawg.conf
    ok "amneziawg installed and loaded"
  fi
}

# ── Update deploy/.env ────────────────────────────────────────────────────────
update_env() {
  local userspace_val=""
  [[ "$MODE" == "userspace" ]] && userspace_val="amneziawg-go"

  if [[ -f "$ENV_FILE" ]]; then
    # Update existing values
    if grep -q "^AWG_USERSPACE_IMPL=" "$ENV_FILE"; then
      sed -i "s|^AWG_USERSPACE_IMPL=.*|AWG_USERSPACE_IMPL=${MODE}|" "$ENV_FILE"
    else
      echo "AWG_USERSPACE_IMPL=${MODE}" >> "$ENV_FILE"
    fi
    if grep -q "^WG_QUICK_USERSPACE_IMPLEMENTATION=" "$ENV_FILE"; then
      sed -i "s|^WG_QUICK_USERSPACE_IMPLEMENTATION=.*|WG_QUICK_USERSPACE_IMPLEMENTATION=${userspace_val}|" "$ENV_FILE"
    else
      echo "WG_QUICK_USERSPACE_IMPLEMENTATION=${userspace_val}" >> "$ENV_FILE"
    fi
  else
    # Create minimal env file
    printf "AWG_USERSPACE_IMPL=%s\nWG_QUICK_USERSPACE_IMPLEMENTATION=%s\n" "$MODE" "$userspace_val" > "$ENV_FILE"
  fi
  ok "deploy/.env updated"
}

# ── Main ──────────────────────────────────────────────────────────────────────
cd "$REPO_DIR"

echo ""
echo -e "${B}── Cascade: switching AWG mode → ${MODE}${N}"
echo ""

if [[ "$MODE" == "userspace" ]]; then
  apply_userspace
else
  apply_kernel
fi

update_env

# Restart container if running
if $COMPOSE_CMD -f "$COMPOSE_FILE" ps --quiet 2>/dev/null | grep -q .; then
  info "Restarting Cascade container..."
  $COMPOSE_CMD -f "$COMPOSE_FILE" down
  $COMPOSE_CMD -f "$COMPOSE_FILE" up -d
  ok "Container restarted"

  sleep 2
  echo ""
  echo -e "${B}── Verification${N}"
  WG_QUICK_VAL=$(docker exec awg-router env 2>/dev/null | grep WG_QUICK || echo "(not found)")
  echo "  WG_QUICK_USERSPACE_IMPLEMENTATION: $(echo "$WG_QUICK_VAL" | cut -d= -f2)"

  if [[ "$MODE" == "userspace" ]]; then
    PROC=$(docker exec awg-router ps aux 2>/dev/null | grep amneziawg-go | grep -v grep || echo "")
    [[ -n "$PROC" ]] && ok "amneziawg-go process running" || info "amneziawg-go will start when first interface is brought up"
  else
    lsmod | grep -q amneziawg && ok "amneziawg kernel module loaded" || warn "Module not loaded — check dmesg"
  fi
else
  info "Container is not running — start with:"
  echo "  $COMPOSE_CMD -f docker-compose.go.yml up -d"
fi

echo ""
ok "Done. Mode: ${MODE}"
echo ""
