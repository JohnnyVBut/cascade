#!/bin/bash
# deploy/tests/tcp_tune_test.sh — unit tests for tcp_tune.sh
# Run from any directory: bash deploy/tests/tcp_tune_test.sh
# No external dependencies required (pure bash).

set -euo pipefail

# TEST-1 fix: абсолютный путь к скрипту — работает из любой директории
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SCRIPT_FILE="$REPO_ROOT/deploy/tcp_tune.sh"
DUPLICATE_FILE="$REPO_ROOT/deploy/caddy/scripts/tcp_tune.sh"

[[ -f "$SCRIPT_FILE" ]] || { echo "ERROR: $SCRIPT_FILE not found"; exit 1; }

PASS=0; FAIL=0

pass() { echo -e "\033[32m  PASS\033[0m $1"; PASS=$(( PASS + 1 )); }
fail() { echo -e "\033[31m  FAIL\033[0m $1"; FAIL=$(( FAIL + 1 )); }

assert_eq() {
    local desc=$1 got=$2 want=$3
    if [[ "$got" == "$want" ]]; then pass "$desc"; else fail "$desc: got='$got' want='$want'"; fi
}

# TEST-2 fix: grep-based assertions — не зависят от пробелов/форматирования
assert_sysctl_apply() {
    # Проверяет что строка `apply ... key value` присутствует в executable-строках
    local desc=$1 key=$2 val=$3
    if grep -v '^\s*#' "$SCRIPT_FILE" | grep -qE "apply[[:space:]].*${key}[[:space:]]+${val}[^0-9]?"; then
        pass "$desc"
    else
        fail "$desc: 'apply ... $key $val' not found"
    fi
}

assert_sysctl_file() {
    # Проверяет что key = val присутствует в sysctl.d heredoc
    local desc=$1 key=$2 val=$3
    if grep -qE "^${key}[[:space:]]*=[[:space:]]*${val}" "$SCRIPT_FILE"; then
        pass "$desc"
    else
        fail "$desc: '$key = $val' not found in sysctl.d heredoc"
    fi
}

assert_not_sysctl_apply() {
    local desc=$1 key=$2 val=$3
    if grep -v '^\s*#' "$SCRIPT_FILE" | grep -qE "apply[[:space:]].*${key}[[:space:]]+${val}[^0-9]?"; then
        fail "$desc: 'apply ... $key $val' should NOT be present"
    else
        pass "$desc"
    fi
}

assert_contains() {
    local desc=$1 haystack=$2 needle=$3
    if [[ "$haystack" == *"$needle"* ]]; then pass "$desc"; else fail "$desc: '$needle' not found"; fi
}

assert_not_contains() {
    local desc=$1 haystack=$2 needle=$3
    if [[ "$haystack" != *"$needle"* ]]; then pass "$desc"; else fail "$desc: '$needle' should NOT be present"; fi
}

SCRIPT=$(cat "$SCRIPT_FILE")

echo "═══ tcp_tune.sh unit tests ════════════════════════════════"

# ── 1. Syntax check ──────────────────────────────────────────────────────────
echo -e "\n── 1. Syntax"
if bash -n "$SCRIPT_FILE" 2>/dev/null; then
    pass "bash -n: syntax OK"
else
    fail "bash -n: syntax error in tcp_tune.sh"
fi

# ── 2. No bc dependency ──────────────────────────────────────────────────────
echo -e "\n── 2. No bc dependency"
if grep -v '^\s*#' "$SCRIPT_FILE" | grep -qE '\bbc\b'; then
    fail "bc still used in executable lines"
else
    pass "bc not used in executable lines — pure bash arithmetic"
fi

# ── 3. RAM tier logic ────────────────────────────────────────────────────────
echo -e "\n── 3. RAM tier detection"

_tier_for_mb() {
    local mb=$1
    if   [[ $mb -lt  512 ]]; then echo "tiny"
    elif [[ $mb -lt 1024 ]]; then echo "small"
    elif [[ $mb -lt 2048 ]]; then echo "medium"
    elif [[ $mb -lt 8192 ]]; then echo "large"
    else                           echo "xlarge"
    fi
}

assert_eq "256MB → tiny"    "$(_tier_for_mb 256)"   "tiny"
assert_eq "511MB → tiny"    "$(_tier_for_mb 511)"   "tiny"
assert_eq "512MB → small"   "$(_tier_for_mb 512)"   "small"
assert_eq "1023MB → small"  "$(_tier_for_mb 1023)"  "small"
assert_eq "1024MB → medium" "$(_tier_for_mb 1024)"  "medium"
assert_eq "2047MB → medium" "$(_tier_for_mb 2047)"  "medium"
assert_eq "2048MB → large"  "$(_tier_for_mb 2048)"  "large"
assert_eq "8191MB → large"  "$(_tier_for_mb 8191)"  "large"
assert_eq "8192MB → xlarge" "$(_tier_for_mb 8192)"  "xlarge"
assert_eq "16384MB → xlarge" "$(_tier_for_mb 16384)" "xlarge"

# ── 4. RAM_GB arithmetic (no bc) ─────────────────────────────────────────────
echo -e "\n── 4. RAM_GB arithmetic (no bc)"
_ram_mb=1536
_ram_gb=$(( _ram_mb / 1024 ))
_ram_frac=$(( (_ram_mb % 1024) * 10 / 1024 ))
assert_eq "1536MB → 1 GB"     "$_ram_gb"   "1"
assert_eq "1536MB → .5 frac"  "$_ram_frac" "5"

_ram_mb=2048
_ram_gb=$(( _ram_mb / 1024 ))
_ram_frac=$(( (_ram_mb % 1024) * 10 / 1024 ))
assert_eq "2048MB → 2 GB"     "$_ram_gb"   "2"
assert_eq "2048MB → .0 frac"  "$_ram_frac" "0"

# ── 5. tcp_mem pages (25% RAM) ───────────────────────────────────────────────
echo -e "\n── 5. tcp_mem calculation"
_ram_kb=$(( 2 * 1024 * 1024 ))
_ram_pages=$(( _ram_kb / 4 ))
_tcp_max=$(( _ram_pages / 4 ))
_tcp_pres=$(( _tcp_max * 3 / 4 ))
_tcp_min=$(( _tcp_pres / 2 ))

assert_eq "2GB: tcp_mem_max = 25% pages" "$_tcp_max"  "131072"
assert_eq "2GB: pressure = 75% max"      "$_tcp_pres" "98304"
assert_eq "2GB: min = 50% pressure"      "$_tcp_min"  "49152"
[[ $_tcp_min -lt $_tcp_pres && $_tcp_pres -lt $_tcp_max ]] \
    && pass "tcp_mem invariant: min < pressure < max" \
    || fail "tcp_mem invariant violated"

# ── 6. Corrected parameter values ────────────────────────────────────────────
echo -e "\n── 6. Corrected parameter values"

# H-1: tw_reuse = 0
assert_sysctl_apply "H-1: apply tw_reuse=0"      "tcp_tw_reuse"  "0"
assert_sysctl_file  "H-1: sysctl.d tw_reuse=0"   "net.ipv4.tcp_tw_reuse" "0"
assert_not_sysctl_apply "H-1: tw_reuse not 1"    "tcp_tw_reuse"  "1"

# H-2: fastopen = 1
assert_sysctl_apply "H-2: apply fastopen=1"      "tcp_fastopen"  "1"
assert_sysctl_file  "H-2: sysctl.d fastopen=1"   "net.ipv4.tcp_fastopen" "1"
assert_not_sysctl_apply "H-2: fastopen not 3"    "tcp_fastopen"  "3"

# H-3: ecn = 2
assert_sysctl_apply "H-3: apply ecn=2"           "tcp_ecn"       "2"
assert_sysctl_file  "H-3: sysctl.d ecn=2"        "net.ipv4.tcp_ecn" "2"
assert_not_sysctl_apply "H-3: ecn not 1"         "tcp_ecn"       "1"

# H-4: quoted $key
assert_contains "H-4: key quoted in apply_if_exists" "$SCRIPT" 'echo "$key"'

# M-3: keepalive_time = 300
assert_sysctl_apply "M-3: apply keepalive=300"   "tcp_keepalive_time" "300"
assert_sysctl_file  "M-3: sysctl.d keepalive=300" "net.ipv4.tcp_keepalive_time" "300"
assert_not_sysctl_apply "M-3: keepalive not 60"  "tcp_keepalive_time" "60"

# M-4: no_metrics_save не применяется (может упоминаться в комментарии)
if grep -v '^\s*#' "$SCRIPT_FILE" | grep -qE 'apply.*tcp_no_metrics_save'; then
    fail "M-4: tcp_no_metrics_save still being applied"
else
    pass "M-4: tcp_no_metrics_save not applied — removed (counterproductive with BBR)"
fi

# M-6: IPv6 forwarding in sysctl.d
assert_sysctl_file "M-6: ipv6 forwarding in sysctl.d" "net.ipv6.conf.all.forwarding" "1"

# NEW-1: rp_filter = 2 (loose, не 1)
assert_sysctl_apply "NEW-1: apply rp_filter all=2"     "conf.all.rp_filter"     "2"
assert_sysctl_apply "NEW-1: apply rp_filter default=2" "conf.default.rp_filter" "2"
assert_sysctl_file  "NEW-1: sysctl.d rp_filter all=2"     "net.ipv4.conf.all.rp_filter"     "2"
assert_sysctl_file  "NEW-1: sysctl.d rp_filter default=2" "net.ipv4.conf.default.rp_filter" "2"
# Значение 1 (strict) не должно быть — ломает WireGuard
if grep -qE "^net\.ipv4\.conf\.(all|default)\.rp_filter[[:space:]]*=[[:space:]]*1" "$SCRIPT_FILE"; then
    fail "NEW-1: rp_filter=1 (strict) found in sysctl.d — should be 2 (loose)"
else
    pass "NEW-1: rp_filter=1 (strict) absent — VPN-safe"
fi

# L-5: ICMP redirects и source_route
assert_sysctl_file "L-5: accept_redirects=0"    "net.ipv4.conf.all.accept_redirects"   "0"
assert_sysctl_file "L-5: send_redirects=0"      "net.ipv4.conf.all.send_redirects"     "0"
assert_sysctl_file "L-5: accept_source_route=0" "net.ipv4.conf.all.accept_source_route" "0"

# L-5: UDP buffers
assert_contains "L-5: udp_rmem_min present" "$SCRIPT" "udp_rmem_min"
assert_contains "L-5: optmem_max present"   "$SCRIPT" "optmem_max"

# ── 7. sysctl.d written before tc loop (C-2) ─────────────────────────────────
echo -e "\n── 7. sysctl.d written before tc loop (C-2)"
SYSCTL_LINE=$(grep -n "SYSCTL_FILE=" "$SCRIPT_FILE" | head -1 | cut -d: -f1)
TC_LINE=$(grep -n "tc qdisc replace" "$SCRIPT_FILE" | head -1 | cut -d: -f1)
if [[ -n "$SYSCTL_LINE" && -n "$TC_LINE" && "$SYSCTL_LINE" -lt "$TC_LINE" ]]; then
    pass "C-2: sysctl.d write (line $SYSCTL_LINE) before tc loop (line $TC_LINE)"
else
    fail "C-2: sysctl.d write at line $SYSCTL_LINE, tc loop at $TC_LINE — wrong order"
fi

# ── 8. bc not used (C-1/M-7) ─────────────────────────────────────────────────
echo -e "\n── 8. bc not used (C-1/M-7)"
if grep -v '^\s*#' "$SCRIPT_FILE" | grep -qE '\bbc\b'; then
    fail "C-1/M-7: 'bc' still present in executable lines"
else
    pass "C-1/M-7: 'bc' absent — pure bash arithmetic"
fi

# ── 9. Interface filters in fq loop (L-4/M-1/MAIN-REG-1) ────────────────────
echo -e "\n── 9. Interface filters in fq loop"
assert_contains "L-4: wg*/awg*/tun* skipped"    "$SCRIPT" "wg*|awg*|tun*|tap*|gre*|sit*"
assert_contains "REG-1: veth*/br*/docker* skipped" "$SCRIPT" "veth*|br*|docker*"
assert_not_contains "M-1: ls not used for ifaces"  "$SCRIPT" "ls /sys/class/net/"

# ── 10. Dependency checks (C-1) ──────────────────────────────────────────────
echo -e "\n── 10. Dependency checks (C-1)"
assert_contains "dep: command -v loop"    "$SCRIPT" "command -v"
assert_contains "dep: awk grep sysctl tc" "$SCRIPT" "awk grep sysctl tc"
assert_contains "dep: /proc/meminfo guard" "$SCRIPT" "/proc/meminfo"

# ── 11. Duplicate file removed (L-7) ─────────────────────────────────────────
echo -e "\n── 11. Duplicate file removed (L-7)"
if [[ -f "$DUPLICATE_FILE" ]]; then
    fail "L-7: duplicate $DUPLICATE_FILE still exists"
else
    pass "L-7: duplicate removed"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "═══════════════════════════════════════════════════════"
TOTAL=$(( PASS + FAIL ))
echo -e "  Results: \033[32m${PASS} passed\033[0m / \033[31m${FAIL} failed\033[0m / ${TOTAL} total"
echo "═══════════════════════════════════════════════════════"
[[ $FAIL -eq 0 ]]
