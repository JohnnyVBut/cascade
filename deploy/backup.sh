#!/usr/bin/env bash
# =============================================================================
# Cascade — Back up ./data before an upgrade
#
# Usage:
#   bash deploy/backup.sh                    # backup config DB only (default)
#   bash deploy/backup.sh --include-metrics  # also back up metrics.db (history)
#   bash deploy/backup.sh --dest /path/dir   # custom backup destination
#
# By default this backs up ONLY what's needed to restore a working server:
# cascade.db (config, users, peers, rules), wireguard.db* (legacy pre-rename
# leftovers, if present — see FIX-GO-23 in the app's migration code), and
# firewall alias *.save files. metrics.db (dashboard history graphs) is
# skipped by default: it is not required to restore functionality and on a
# server that has been running a while it is routinely 10-100x larger than
# everything else combined (SQLite's DELETE-based 30-day retention frees
# rows but does not shrink the file — see internal/metrics/collector.go).
# On a small VPS, blindly copying the whole data/ directory can double its
# disk usage and run the host out of space. Pass --include-metrics if you
# specifically want the history graphs preserved too.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DATA_DIR="$REPO_DIR/data"
CONTAINER="cascade"

G='\033[0;32m'; Y='\033[1;33m'; R='\033[0;31m'; B='\033[0;34m'; N='\033[0m'
ok()   { echo -e "  ${G}✓${N} $*"; }
info() { echo -e "  ${B}→${N} $*"; }
warn() { echo -e "  ${Y}⚠${N} $*"; }
fail() { echo -e "  ${R}✗${N} $*"; exit 1; }

# ── Parse args ────────────────────────────────────────────────────────────────
INCLUDE_METRICS=0
DEST=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --include-metrics) INCLUDE_METRICS=1; shift ;;
    --dest)            DEST="$2"; shift 2 ;;
    -h|--help)
      echo "Usage: bash deploy/backup.sh [--include-metrics] [--dest /path/dir]"
      echo ""
      echo "  --include-metrics   Also back up metrics.db (dashboard history graphs)."
      echo "                      Skipped by default — often 10-100x larger than"
      echo "                      everything else and not needed to restore function."
      echo "  --dest DIR          Backup destination directory (default:"
      echo "                      ./data.backup-YYYYMMDD-HHMMSS next to ./data)."
      exit 0
      ;;
    *) fail "Unknown argument: $1. See --help." ;;
  esac
done

[[ -z "$DEST" ]] && DEST="${DATA_DIR}.backup-$(date +%Y%m%d-%H%M%S)"

# ── Sanity checks ─────────────────────────────────────────────────────────────
[[ -d "$DATA_DIR" ]] || fail "No data directory found at $DATA_DIR — nothing to back up."
[[ -f "$DATA_DIR/cascade.db" || -f "$DATA_DIR/wireguard.db" ]] || fail "No cascade.db/wireguard.db in $DATA_DIR — nothing to back up (fresh install?)."
[[ -e "$DEST" ]] && fail "Destination already exists: $DEST"

echo ""
echo -e "${B}── Cascade: backing up ./data${N}"
echo ""

# ── Build the list of files to back up ────────────────────────────────────────
FILES=()
for f in cascade.db cascade.db-wal cascade.db-shm wireguard.db wireguard.db-wal wireguard.db-shm; do
  [[ -f "$DATA_DIR/$f" ]] && FILES+=("$f")
done
while IFS= read -r -d '' f; do
  FILES+=("$(basename "$f")")
done < <(find "$DATA_DIR" -maxdepth 1 -name '*.save' -print0 2>/dev/null)

if [[ "$INCLUDE_METRICS" -eq 1 ]]; then
  for f in metrics.db metrics.db-wal metrics.db-shm; do
    [[ -f "$DATA_DIR/$f" ]] && FILES+=("$f")
  done
  info "Including metrics.db (dashboard history) — this can be large."
else
  if [[ -f "$DATA_DIR/metrics.db" ]]; then
    SKIP_SIZE=$(du -sh "$DATA_DIR/metrics.db" 2>/dev/null | cut -f1)
    info "Skipping metrics.db (${SKIP_SIZE:-?}) — not needed to restore. Use --include-metrics to keep it."
  fi
fi

[[ ${#FILES[@]} -eq 0 ]] && fail "Nothing to back up — no known files found in $DATA_DIR."

# ── Preflight: does the destination filesystem have room? ─────────────────────
NEED_KB=0
for f in "${FILES[@]}"; do
  if [[ -f "$DATA_DIR/$f" ]]; then
    SIZE_KB=$(du -k "$DATA_DIR/$f" | cut -f1)
    NEED_KB=$(( NEED_KB + SIZE_KB ))
  fi
done
# +20% margin — filesystem block overhead, and so this isn't a razor-thin pass/fail.
NEED_KB_WITH_MARGIN=$(( NEED_KB * 12 / 10 ))

DEST_PARENT="$(dirname "$DEST")"
mkdir -p "$DEST_PARENT" 2>/dev/null || true
# `|| true` on the whole pipeline: an unsupported `df` flag (or any other
# failure) must not abort the script via `set -e` — fall through to the
# "could not determine" warning below instead of a hard crash.
AVAIL_KB=$(df -k --output=avail "$DEST_PARENT" 2>/dev/null | tail -1 | tr -d ' ') || AVAIL_KB=""

if [[ -z "$AVAIL_KB" ]]; then
  warn "Could not determine available disk space at $DEST_PARENT — proceeding without the preflight check."
elif [[ "$AVAIL_KB" -lt "$NEED_KB_WITH_MARGIN" ]]; then
  fail "Not enough disk space: need ~$(( NEED_KB_WITH_MARGIN / 1024 ))MB (with margin), only $(( AVAIL_KB / 1024 ))MB available at $DEST_PARENT.
      Free up space first (see README troubleshooting), or run with --dest pointing
      at a filesystem with more room. metrics.db is already excluded by default —
      if you passed --include-metrics, try again without it."
else
  ok "Disk space check passed: need ~$(( NEED_KB_WITH_MARGIN / 1024 ))MB, $(( AVAIL_KB / 1024 ))MB available."
fi

# ── Checkpoint the WAL before copying, if the container is running ────────────
# SQLite WAL mode means recent transactions may still be sitting in
# cascade.db-wal, not yet folded into cascade.db — copying without a
# checkpoint first can silently drop the most recent writes from the backup.
if docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "$CONTAINER"; then
  info "Container is running — checkpointing WAL before copying (no downtime)."
  for db in cascade.db wireguard.db; do
    if [[ -f "$DATA_DIR/$db" ]]; then
      docker exec "$CONTAINER" sqlite3 "/etc/wireguard/data/$db" "PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null 2>&1 \
        && ok "Checkpointed $db" \
        || warn "Could not checkpoint $db (sqlite3 missing in container, or db in use) — backup may miss the most recent writes."
    fi
  done
  if [[ "$INCLUDE_METRICS" -eq 1 && -f "$DATA_DIR/metrics.db" ]]; then
    docker exec "$CONTAINER" sqlite3 "/etc/wireguard/data/metrics.db" "PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null 2>&1 \
      && ok "Checkpointed metrics.db" \
      || warn "Could not checkpoint metrics.db — backup may miss the most recent writes."
  fi
else
  info "Container is not running — copying directly (no checkpoint needed, nothing is writing)."
fi

# ── Copy ────────────────────────────────────────────────────────────────────
mkdir -p "$DEST"
for f in "${FILES[@]}"; do
  cp -p "$DATA_DIR/$f" "$DEST/$f"
done

TOTAL_SIZE=$(du -sh "$DEST" 2>/dev/null | cut -f1)
echo ""
ok "Backup complete: $DEST (${TOTAL_SIZE:-?})"
echo "  Files: ${FILES[*]}"
echo ""
echo "  To restore: stop the container, replace the contents of $DATA_DIR"
echo "  with this backup's files, then start the container again."
echo ""
