#!/usr/bin/env bash
# =============================================================================
# Cascade — one-shot fresh-VPS bootstrap: Userspace mode (amneziawg-go), Host network
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/JohnnyVBut/cascade/master/deploy/quickstart-userspace.sh | sudo bash
#   curl -fsSL https://raw.githubusercontent.com/JohnnyVBut/cascade/master/deploy/quickstart-userspace.sh | sudo bash -s -- --staging
#   # or, after cloning the repo yourself:
#   sudo bash deploy/quickstart-userspace.sh [--staging]
#
# --staging issues a Let's Encrypt STAGING certificate (untrusted by browsers,
# no rate limits) — use it for testing. Omit it for a real production install.
# =============================================================================
set -euo pipefail

[[ "$(id -u)" -ne 0 ]] && { echo "Run as root: sudo bash $0"; exit 1; }

STAGING_ARGS=()
for arg in "$@"; do
  [[ "$arg" == "--staging" ]] && STAGING_ARGS+=(--staging)
done

REPO_URL="https://github.com/JohnnyVBut/cascade.git"
REPO_DIR="${REPO_DIR:-$HOME/cascade}"

if ! command -v git &>/dev/null; then
  echo "→ Installing git..."
  apt-get update -qq && apt-get install -y -qq git
fi

if [[ -d "$REPO_DIR/.git" ]]; then
  echo "→ $REPO_DIR already exists — pulling latest instead of re-cloning"
  git -C "$REPO_DIR" pull origin master
else
  echo "→ Cloning Cascade into $REPO_DIR"
  git clone "$REPO_URL" "$REPO_DIR"
fi

cd "$REPO_DIR"

export AWG_USERSPACE_IMPL=amneziawg-go
export NETWORK_MODE=host

echo "→ Running setup.sh --yes ${STAGING_ARGS[*]} (Userspace mode, Host network)"
bash deploy/setup.sh --yes "${STAGING_ARGS[@]}"
