#!/usr/bin/env bash
# =============================================================================
# Cascade — OVS interface detach helper
# Removes the OVS port from a running container.
#
# Usage:
#   bash deploy/ovs/detach.sh [--container NAME] [--bridge NAME] [--iface NAME]
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/../.env"

[[ -f "$ENV_FILE" ]] && source "$ENV_FILE"

CONTAINER=${CASCADE_CONTAINER:-cascade}
OVS_BRIDGE=${OVS_BRIDGE:-br-trunk}
CONTAINER_IFACE=${OVS_IFACE:-eth0}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --container) CONTAINER="$2";       shift 2 ;;
    --bridge)    OVS_BRIDGE="$2";      shift 2 ;;
    --iface)     CONTAINER_IFACE="$2"; shift 2 ;;
    *) echo "Unknown: $1"; exit 1 ;;
  esac
done

command -v ovs-docker &>/dev/null || { echo "ovs-docker not found"; exit 1; }

echo "Detaching $CONTAINER_IFACE from $OVS_BRIDGE on container $CONTAINER..."
ovs-docker del-port "$OVS_BRIDGE" "$CONTAINER_IFACE" "$CONTAINER"
echo "Done."
