#!/bin/sh
# Cascade container entrypoint.
#
# Responsibilities:
#   1. Enable IP forwarding and src_valid_mark in this network namespace
#      (works in both host-network and isolated/OVS modes).
#   2. In isolated mode (WAIT_FOR_NETWORK=1), wait for a default route to
#      appear before starting Cascade.  When the container starts with
#      --network none and ovs-docker attaches the interface afterwards,
#      Cascade must not run tunnel.Init() before the route is available —
#      otherwise the WireGuard PostUp getIsp probe fails.
#
# Environment variables:
#   WAIT_FOR_NETWORK   1 = wait for default route (default: 0 in host mode,
#                          set to 1 automatically in docker-compose.isolated.yml)
#   NETWORK_WAIT_TIMEOUT  seconds to wait before giving up (default: 60)

set -e

# ── 1. Kernel network parameters ─────────────────────────────────────────────
# These are safe to set in any network mode:
#   - In --network host: sets them on the host (same as manual sysctl)
#   - In isolated netns: sets them only for the container namespace
sysctl -w net.ipv4.ip_forward=1              2>/dev/null || true
sysctl -w net.ipv4.conf.all.src_valid_mark=1 2>/dev/null || true

# ── 2. Wait for network (isolated/OVS mode) ───────────────────────────────────
WAIT=${WAIT_FOR_NETWORK:-0}
TIMEOUT=${NETWORK_WAIT_TIMEOUT:-60}

if [ "$WAIT" = "1" ]; then
  echo "[entrypoint] Waiting for default route (OVS attach expected within ${TIMEOUT}s)..."
  i=0
  while ! ip route show default 2>/dev/null | grep -q '^default'; do
    sleep 1
    i=$((i + 1))
    if [ "$i" -ge "$TIMEOUT" ]; then
      echo "[entrypoint] Warning: no default route after ${TIMEOUT}s — starting anyway"
      break
    fi
  done
  ROUTE=$(ip route show default 2>/dev/null || echo "none")
  echo "[entrypoint] Network ready: $ROUTE"
fi

# ── 3. Exec Cascade ───────────────────────────────────────────────────────────
exec cascade "$@"
