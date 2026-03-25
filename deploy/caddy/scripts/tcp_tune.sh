#!/bin/bash
# tcp-tune.sh — автонастройка TCP буферов + VPN-оптимизации

set -e

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

info()  { echo -e "${CYAN}[INFO]${NC} $*"; }
ok()    { echo -e "${GREEN}[ OK ]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
skip()  { echo -e "  ${YELLOW}SKIP${NC}  $*"; }
die()   { echo -e "${RED}[ERR ]${NC} $*" >&2; exit 1; }

[[ $EUID -ne 0 ]] && die "Нужен root"

# ── RAM ────────────────────────────────────────────────────────────────────
RAM_KB=$(grep MemTotal /proc/meminfo | awk '{print $2}')
RAM_MB=$((RAM_KB / 1024))
RAM_GB=$(echo "scale=1; $RAM_MB / 1024" | bc)
# страниц памяти (4KB каждая)
RAM_PAGES=$((RAM_KB / 4))

info "RAM: ${RAM_MB} MB (${RAM_GB} GB) = ${RAM_PAGES} pages"

# ── Тиры по RAM ───────────────────────────────────────────────────────────
# tcp_mem: min / pressure / max в страницах
# max ≈ 25% RAM, pressure ≈ 75% max, min ≈ 50% pressure
# core/tcp буферы ≈ 15% RAM, но не больше разумного потолка

if [[ $RAM_MB -lt 512 ]]; then
    TIER="tiny (<512MB)"
    CORE_RMAX=$((512  * 1024))
    CORE_WMAX=$((512  * 1024))
    CORE_RDEF=$((64   * 1024))
    CORE_WDEF=$((64   * 1024))
    TCP_RMIN=4096; TCP_RDEF=$((32 * 1024)); TCP_RMAX=$((512 * 1024))
    TCP_WMIN=4096; TCP_WDEF=$((16 * 1024)); TCP_WMAX=$((512 * 1024))
    BACKLOG=256; SOMAXCONN=512; TW_BUCKETS=8192; NETDEV_BACKLOG=1000
    MAX_ORPHANS=8192

elif [[ $RAM_MB -lt 1024 ]]; then
    TIER="small (512MB–1GB)"
    CORE_RMAX=$((4  * 1024 * 1024))
    CORE_WMAX=$((4  * 1024 * 1024))
    CORE_RDEF=$((128 * 1024))
    CORE_WDEF=$((128 * 1024))
    TCP_RMIN=4096; TCP_RDEF=$((65 * 1024)); TCP_RMAX=$((4 * 1024 * 1024))
    TCP_WMIN=4096; TCP_WDEF=$((32 * 1024)); TCP_WMAX=$((4 * 1024 * 1024))
    BACKLOG=512; SOMAXCONN=1024; TW_BUCKETS=32768; NETDEV_BACKLOG=2000
    MAX_ORPHANS=16384

elif [[ $RAM_MB -lt 2048 ]]; then
    TIER="medium (1–2GB)"
    CORE_RMAX=$((8  * 1024 * 1024))
    CORE_WMAX=$((8  * 1024 * 1024))
    CORE_RDEF=$((256 * 1024))
    CORE_WDEF=$((256 * 1024))
    TCP_RMIN=4096; TCP_RDEF=$((87 * 1024)); TCP_RMAX=$((8 * 1024 * 1024))
    TCP_WMIN=4096; TCP_WDEF=$((65 * 1024)); TCP_WMAX=$((8 * 1024 * 1024))
    BACKLOG=1024; SOMAXCONN=2048; TW_BUCKETS=131072; NETDEV_BACKLOG=3000
    MAX_ORPHANS=32768

elif [[ $RAM_MB -lt 8192 ]]; then
    TIER="large (2–8GB)"
    CORE_RMAX=$((16 * 1024 * 1024))
    CORE_WMAX=$((16 * 1024 * 1024))
    CORE_RDEF=$((512 * 1024))
    CORE_WDEF=$((512 * 1024))
    TCP_RMIN=4096; TCP_RDEF=$((87 * 1024)); TCP_RMAX=$((16 * 1024 * 1024))
    TCP_WMIN=4096; TCP_WDEF=$((65 * 1024)); TCP_WMAX=$((16 * 1024 * 1024))
    BACKLOG=4096; SOMAXCONN=8192; TW_BUCKETS=262144; NETDEV_BACKLOG=5000
    MAX_ORPHANS=65536

else
    TIER="xlarge (>8GB)"
    CORE_RMAX=$((32 * 1024 * 1024))
    CORE_WMAX=$((32 * 1024 * 1024))
    CORE_RDEF=$((1024 * 1024))
    CORE_WDEF=$((1024 * 1024))
    TCP_RMIN=4096; TCP_RDEF=$((87 * 1024)); TCP_RMAX=$((32 * 1024 * 1024))
    TCP_WMIN=4096; TCP_WDEF=$((65 * 1024)); TCP_WMAX=$((32 * 1024 * 1024))
    BACKLOG=8192; SOMAXCONN=65535; TW_BUCKETS=1440000; NETDEV_BACKLOG=5000
    MAX_ORPHANS=131072
fi

# tcp_mem считается от реального объёма RAM (в страницах)
TCP_MEM_MAX=$(( RAM_PAGES / 4 ))       # 25% RAM
TCP_MEM_PRESSURE=$(( TCP_MEM_MAX * 3 / 4 ))
TCP_MEM_MIN=$(( TCP_MEM_PRESSURE / 2 ))

info "Tier: ${BOLD}${TIER}${NC}"
info "tcp_mem: ${TCP_MEM_MIN} ${TCP_MEM_PRESSURE} ${TCP_MEM_MAX} pages \
(max ≈ $(( TCP_MEM_MAX * 4 / 1024 )) MB)"

# ── Проверка версии ядра для deprecated параметров ─────────────────────────
KERNEL=$(uname -r)
KERNEL_MAJOR=$(echo $KERNEL | cut -d. -f1)
KERNEL_MINOR=$(echo $KERNEL | cut -d. -f2)

kernel_ge() {
    local major=$1 minor=$2
    [[ $KERNEL_MAJOR -gt $major ]] || \
    [[ $KERNEL_MAJOR -eq $major && $KERNEL_MINOR -ge $minor ]]
}

info "Ядро: ${KERNEL}"

# ── BBR ────────────────────────────────────────────────────────────────────
BBR_OK=0
if modprobe tcp_bbr 2>/dev/null; then
    BBR_OK=1
    ok "tcp_bbr модуль загружен"
else
    warn "tcp_bbr недоступен, остаётся cubic"
fi

# ── apply / skip ───────────────────────────────────────────────────────────
apply() {
    local key=$1 val=$2
    if sysctl -qw "${key}=${val}" 2>/dev/null; then
        ok "${key} = ${val}"
    else
        warn "Не удалось применить: ${key} (не существует в этом ядре?)"
    fi
}

apply_if_exists() {
    local key=$1 val=$2
    if [[ -f "/proc/sys/$(echo $key | tr . /)" ]]; then
        apply "$key" "$val"
    else
        skip "${key} — не существует в ядре ${KERNEL}"
    fi
}

# ── Применяем ─────────────────────────────────────────────────────────────
echo -e "\n${BOLD}── Буферы (глобальные) ──────────────────────────────${NC}"
apply net.core.rmem_max         $CORE_RMAX
apply net.core.wmem_max         $CORE_WMAX
apply net.core.rmem_default     $CORE_RDEF
apply net.core.wmem_default     $CORE_WDEF

echo -e "\n${BOLD}── TCP буферы на сокет ──────────────────────────────${NC}"
apply net.ipv4.tcp_rmem         "${TCP_RMIN} ${TCP_RDEF} ${TCP_RMAX}"
apply net.ipv4.tcp_wmem         "${TCP_WMIN} ${TCP_WDEF} ${TCP_WMAX}"
apply net.ipv4.tcp_mem          "${TCP_MEM_MIN} ${TCP_MEM_PRESSURE} ${TCP_MEM_MAX}"

echo -e "\n${BOLD}── Congestion control ───────────────────────────────${NC}"
apply net.core.default_qdisc fq
if [[ $BBR_OK -eq 1 ]]; then
    apply net.ipv4.tcp_congestion_control bbr
else
    apply net.ipv4.tcp_congestion_control cubic
fi

echo -e "\n${BOLD}── Очереди ──────────────────────────────────────────${NC}"
apply net.core.netdev_max_backlog  $NETDEV_BACKLOG
apply net.core.somaxconn           $SOMAXCONN
apply net.ipv4.tcp_max_syn_backlog $BACKLOG

echo -e "\n${BOLD}── TIME_WAIT ─────────────────────────────────────────${NC}"
apply net.ipv4.tcp_max_tw_buckets $TW_BUCKETS
apply net.ipv4.tcp_tw_reuse       1
apply net.ipv4.tcp_fin_timeout    15
# tcp_tw_recycle — УДАЛЁН в 4.12, не применяем
if kernel_ge 4 12; then
    skip "net.ipv4.tcp_tw_recycle — удалён в ядре 4.12+"
else
    apply net.ipv4.tcp_tw_recycle 0
fi

echo -e "\n${BOLD}── TCP фичи ─────────────────────────────────────────${NC}"
apply net.ipv4.tcp_fastopen           3
apply net.ipv4.tcp_mtu_probing        1
apply net.ipv4.tcp_slow_start_after_idle 0
apply net.ipv4.tcp_sack               1
apply net.ipv4.tcp_dsack              1
apply net.ipv4.tcp_timestamps         1
apply net.ipv4.tcp_window_scaling     1
apply net.ipv4.tcp_no_metrics_save    1
apply net.ipv4.tcp_ecn                1
apply net.ipv4.tcp_keepalive_time     60
apply net.ipv4.tcp_keepalive_intvl    10
apply net.ipv4.tcp_keepalive_probes   6

# tcp_low_latency — удалён в 4.14
if kernel_ge 4 14; then
    skip "net.ipv4.tcp_low_latency — удалён в ядре 4.14+"
else
    apply net.ipv4.tcp_low_latency 1
fi

# tcp_fack — удалён в 4.15 (поглощён SACK)
if kernel_ge 4 15; then
    skip "net.ipv4.tcp_fack — удалён в ядре 4.15+"
else
    apply net.ipv4.tcp_fack 1
fi

echo -e "\n${BOLD}── Защита (SYN-флуд, orphans) ───────────────────────${NC}"
apply net.ipv4.tcp_syncookies   1
apply net.ipv4.tcp_max_orphans  $MAX_ORPHANS
apply net.ipv4.tcp_syn_retries  3
apply net.ipv4.tcp_synack_retries 3

echo -e "\n${BOLD}── Forwarding (VPN) ─────────────────────────────────${NC}"
apply net.ipv4.ip_forward 1
apply_if_exists net.ipv6.conf.all.forwarding 1

echo -e "\n${BOLD}── VM (меньше свопинга) ─────────────────────────────${NC}"
apply vm.swappiness          10
apply vm.vfs_cache_pressure  50

# ── fq на все интерфейсы ───────────────────────────────────────────────────
echo -e "\n${BOLD}── qdisc fq на интерфейсах ──────────────────────────${NC}"
for iface in $(ls /sys/class/net/ | grep -v lo); do
    if tc qdisc replace dev "$iface" root fq 2>/dev/null; then
        ok "fq → $iface"
    else
        warn "Не удалось заменить qdisc на $iface"
    fi
done

# ── сохранить в sysctl.d ───────────────────────────────────────────────────
SYSCTL_FILE="/etc/sysctl.d/99-tcp-tuning.conf"
echo -e "\n${BOLD}── Сохранение в ${SYSCTL_FILE} ───────────────────────${NC}"

cat > "$SYSCTL_FILE" << EOF
# Auto-generated by tcp-tune.sh
# RAM: ${RAM_MB} MB | Tier: ${TIER} | Kernel: ${KERNEL}
# $(date)

# ── Буферы (глобальные) ───────────────────────────────────────────────────
net.core.rmem_max             = $CORE_RMAX
net.core.wmem_max             = $CORE_WMAX
net.core.rmem_default         = $CORE_RDEF
net.core.wmem_default         = $CORE_WDEF

# ── TCP буферы ────────────────────────────────────────────────────────────
net.ipv4.tcp_rmem             = $TCP_RMIN $TCP_RDEF $TCP_RMAX
net.ipv4.tcp_wmem             = $TCP_WMIN $TCP_WDEF $TCP_WMAX
net.ipv4.tcp_mem              = $TCP_MEM_MIN $TCP_MEM_PRESSURE $TCP_MEM_MAX

# ── Congestion control ────────────────────────────────────────────────────
net.core.default_qdisc        = fq
net.ipv4.tcp_congestion_control = $([ $BBR_OK -eq 1 ] && echo bbr || echo cubic)

# ── Очереди ───────────────────────────────────────────────────────────────
net.core.netdev_max_backlog   = $NETDEV_BACKLOG
net.core.somaxconn            = $SOMAXCONN
net.ipv4.tcp_max_syn_backlog  = $BACKLOG

# ── TIME_WAIT ─────────────────────────────────────────────────────────────
net.ipv4.tcp_max_tw_buckets   = $TW_BUCKETS
net.ipv4.tcp_tw_reuse         = 1
net.ipv4.tcp_fin_timeout      = 15

# ── TCP фичи ──────────────────────────────────────────────────────────────
net.ipv4.tcp_fastopen         = 3
net.ipv4.tcp_mtu_probing      = 1
net.ipv4.tcp_slow_start_after_idle = 0
net.ipv4.tcp_sack             = 1
net.ipv4.tcp_dsack            = 1
net.ipv4.tcp_timestamps       = 1
net.ipv4.tcp_window_scaling   = 1
net.ipv4.tcp_no_metrics_save  = 1
net.ipv4.tcp_ecn              = 1
net.ipv4.tcp_keepalive_time   = 60
net.ipv4.tcp_keepalive_intvl  = 10
net.ipv4.tcp_keepalive_probes = 6

# ── Защита ────────────────────────────────────────────────────────────────
net.ipv4.tcp_syncookies       = 1
net.ipv4.tcp_max_orphans      = $MAX_ORPHANS
net.ipv4.tcp_syn_retries      = 3
net.ipv4.tcp_synack_retries   = 3

# ── VPN forwarding ────────────────────────────────────────────────────────
net.ipv4.ip_forward           = 1

# ── VM ────────────────────────────────────────────────────────────────────
vm.swappiness                 = 10
vm.vfs_cache_pressure         = 50

# NOTE: следующие параметры НЕ включены — удалены из ядра:
# net.ipv4.tcp_low_latency  (удалён в 4.14)
# net.ipv4.tcp_fack         (удалён в 4.15)
# net.ipv4.tcp_tw_recycle   (удалён в 4.12)
EOF

ok "Сохранено → ${SYSCTL_FILE}"

# ── итог ───────────────────────────────────────────────────────────────────
echo -e "\n${GREEN}${BOLD}══ Готово ══════════════════════════════════════════${NC}"
printf "  %-14s %s\n" "RAM:"    "${RAM_MB} MB → tier: ${TIER}"
printf "  %-14s %s\n" "rmem:"   "$(( CORE_RMAX / 1024 / 1024 )) MB max"
printf "  %-14s %s\n" "wmem:"   "$(( CORE_WMAX / 1024 / 1024 )) MB max"
printf "  %-14s %s\n" "tcp_mem:" "$(( TCP_MEM_MAX * 4 / 1024 )) MB max ($(( TCP_MEM_MAX )) pages)"
printf "  %-14s %s\n" "BBR:"    "$([ $BBR_OK -eq 1 ] && echo enabled || echo 'cubic fallback')"
printf "  %-14s %s\n" "Config:" "${SYSCTL_FILE}"