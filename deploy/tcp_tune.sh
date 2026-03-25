#!/bin/bash
# tcp_tune.sh — автонастройка TCP буферов + VPN-оптимизации
# Требует: bash 4+, awk, grep, sysctl, tc (iproute2)
# Целевая платформа: Ubuntu 22.04+ (ядро 5.15+)

set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

info()  { echo -e "${CYAN}[INFO]${NC} $*"; }
ok()    { echo -e "${GREEN}[ OK ]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
skip()  { echo -e "  ${YELLOW}SKIP${NC}  $*"; }
die()   { echo -e "${RED}[ERR ]${NC} $*" >&2; exit 1; }

# ── Проверки окружения ──────────────────────────────────────────────────────
[[ $EUID -ne 0 ]] && die "Нужен root"

for _cmd in awk grep sysctl tc; do
    command -v "$_cmd" >/dev/null 2>&1 || die "Требуется '$_cmd' — не найден. Установите iproute2 / procps."
done

[[ -f /proc/meminfo ]] || die "/proc/meminfo недоступен — скрипт требует Linux с procfs"

# ── RAM (чистый bash, без bc) ───────────────────────────────────────────────
RAM_KB=$(grep MemTotal /proc/meminfo | awk '{print $2}')
RAM_MB=$(( RAM_KB / 1024 ))
RAM_GB=$(( RAM_MB / 1024 ))
RAM_GB_FRAC=$(( (RAM_MB % 1024) * 10 / 1024 ))   # одна цифра после точки
# страниц памяти (4KB каждая)
RAM_PAGES=$(( RAM_KB / 4 ))

info "RAM: ${RAM_MB} MB (${RAM_GB}.${RAM_GB_FRAC} GB) = ${RAM_PAGES} pages"

# ── Тиры по RAM ────────────────────────────────────────────────────────────
# tcp_mem: min / pressure / max в страницах
# max ≈ 25% RAM, pressure ≈ 75% max, min ≈ 50% pressure
# core/tcp буферы масштабируются по RAM, но ограничены разумным потолком

if [[ $RAM_MB -lt 512 ]]; then
    TIER="tiny (<512MB)"
    CORE_RMAX=$(( 512 * 1024 ))
    CORE_WMAX=$(( 512 * 1024 ))
    CORE_RDEF=$(( 64  * 1024 ))
    CORE_WDEF=$(( 64  * 1024 ))
    TCP_RMIN=4096; TCP_RDEF=$(( 32 * 1024 )); TCP_RMAX=$(( 512 * 1024 ))
    TCP_WMIN=4096; TCP_WDEF=$(( 16 * 1024 )); TCP_WMAX=$(( 512 * 1024 ))
    # UDP буферы для WireGuard (UDP-first VPN)
    UDP_RMIN=4096; UDP_RDEF=$(( 64  * 1024 )); UDP_RMAX=$(( 512 * 1024 ))
    BACKLOG=256; SOMAXCONN=512; TW_BUCKETS=8192; NETDEV_BACKLOG=1000
    MAX_ORPHANS=8192

elif [[ $RAM_MB -lt 1024 ]]; then
    TIER="small (512MB–1GB)"
    CORE_RMAX=$(( 4  * 1024 * 1024 ))
    CORE_WMAX=$(( 4  * 1024 * 1024 ))
    CORE_RDEF=$(( 128 * 1024 ))
    CORE_WDEF=$(( 128 * 1024 ))
    TCP_RMIN=4096; TCP_RDEF=$(( 65 * 1024 )); TCP_RMAX=$(( 4 * 1024 * 1024 ))
    TCP_WMIN=4096; TCP_WDEF=$(( 32 * 1024 )); TCP_WMAX=$(( 4 * 1024 * 1024 ))
    UDP_RMIN=4096; UDP_RDEF=$(( 256 * 1024 )); UDP_RMAX=$(( 4 * 1024 * 1024 ))
    BACKLOG=512; SOMAXCONN=1024; TW_BUCKETS=32768; NETDEV_BACKLOG=2000
    MAX_ORPHANS=16384

elif [[ $RAM_MB -lt 2048 ]]; then
    TIER="medium (1–2GB)"
    CORE_RMAX=$(( 8  * 1024 * 1024 ))
    CORE_WMAX=$(( 8  * 1024 * 1024 ))
    CORE_RDEF=$(( 256 * 1024 ))
    CORE_WDEF=$(( 256 * 1024 ))
    TCP_RMIN=4096; TCP_RDEF=$(( 87 * 1024 )); TCP_RMAX=$(( 8 * 1024 * 1024 ))
    TCP_WMIN=4096; TCP_WDEF=$(( 65 * 1024 )); TCP_WMAX=$(( 8 * 1024 * 1024 ))
    UDP_RMIN=4096; UDP_RDEF=$(( 512 * 1024 )); UDP_RMAX=$(( 8 * 1024 * 1024 ))
    BACKLOG=1024; SOMAXCONN=2048; TW_BUCKETS=131072; NETDEV_BACKLOG=3000
    MAX_ORPHANS=32768

elif [[ $RAM_MB -lt 8192 ]]; then
    TIER="large (2–8GB)"
    CORE_RMAX=$(( 16 * 1024 * 1024 ))
    CORE_WMAX=$(( 16 * 1024 * 1024 ))
    CORE_RDEF=$(( 512 * 1024 ))
    CORE_WDEF=$(( 512 * 1024 ))
    TCP_RMIN=4096; TCP_RDEF=$(( 87 * 1024 )); TCP_RMAX=$(( 16 * 1024 * 1024 ))
    TCP_WMIN=4096; TCP_WDEF=$(( 65 * 1024 )); TCP_WMAX=$(( 16 * 1024 * 1024 ))
    UDP_RMIN=4096; UDP_RDEF=$(( 1024 * 1024 )); UDP_RMAX=$(( 16 * 1024 * 1024 ))
    BACKLOG=4096; SOMAXCONN=8192; TW_BUCKETS=262144; NETDEV_BACKLOG=5000
    MAX_ORPHANS=65536

else
    TIER="xlarge (>8GB)"
    CORE_RMAX=$(( 32 * 1024 * 1024 ))
    CORE_WMAX=$(( 32 * 1024 * 1024 ))
    CORE_RDEF=$(( 1024 * 1024 ))
    CORE_WDEF=$(( 1024 * 1024 ))
    TCP_RMIN=4096; TCP_RDEF=$(( 87 * 1024 )); TCP_RMAX=$(( 32 * 1024 * 1024 ))
    TCP_WMIN=4096; TCP_WDEF=$(( 65 * 1024 )); TCP_WMAX=$(( 32 * 1024 * 1024 ))
    UDP_RMIN=4096; UDP_RDEF=$(( 2 * 1024 * 1024 )); UDP_RMAX=$(( 32 * 1024 * 1024 ))
    # 65535 = максимум ядра для somaxconn до 5.4; выше не имеет смысла
    BACKLOG=8192; SOMAXCONN=65535; TW_BUCKETS=1440000; NETDEV_BACKLOG=5000
    MAX_ORPHANS=131072
fi

# tcp_mem считается от реального объёма RAM (в страницах)
TCP_MEM_MAX=$(( RAM_PAGES / 4 ))          # 25% RAM
TCP_MEM_PRESSURE=$(( TCP_MEM_MAX * 3 / 4 ))
TCP_MEM_MIN=$(( TCP_MEM_PRESSURE / 2 ))

info "Tier: ${BOLD}${TIER}${NC}"
info "tcp_mem: ${TCP_MEM_MIN} ${TCP_MEM_PRESSURE} ${TCP_MEM_MAX} pages \
(max ≈ $(( TCP_MEM_MAX * 4 / 1024 )) MB)"

# ── Версия ядра ─────────────────────────────────────────────────────────────
KERNEL=$(uname -r)
info "Ядро: ${KERNEL}"

# ── BBR ─────────────────────────────────────────────────────────────────────
BBR_OK=0
if modprobe tcp_bbr 2>/dev/null; then
    BBR_OK=1
    ok "tcp_bbr модуль загружен"
else
    warn "tcp_bbr недоступен, остаётся cubic"
fi

# ── Вспомогательные функции ─────────────────────────────────────────────────
apply() {
    local key=$1 val=$2
    if sysctl -qw "${key}=${val}" 2>/dev/null; then
        ok "${key} = ${val}"
    else
        warn "Не удалось применить: ${key} (не существует в этом ядре?)"
    fi
}

# H-4 fix: "$key" quoted inside subshell
apply_if_exists() {
    local key=$1 val=$2
    if [[ -f "/proc/sys/$(echo "$key" | tr . /)" ]]; then
        apply "$key" "$val"
    else
        skip "${key} — не существует в ядре ${KERNEL}"
    fi
}

# ── Применяем sysctl параметры ──────────────────────────────────────────────
echo -e "\n${BOLD}── Буферы (глобальные) ──────────────────────────────${NC}"
apply net.core.rmem_max         "$CORE_RMAX"
apply net.core.wmem_max         "$CORE_WMAX"
apply net.core.rmem_default     "$CORE_RDEF"
apply net.core.wmem_default     "$CORE_WDEF"

echo -e "\n${BOLD}── TCP буферы на сокет ──────────────────────────────${NC}"
apply net.ipv4.tcp_rmem         "${TCP_RMIN} ${TCP_RDEF} ${TCP_RMAX}"
apply net.ipv4.tcp_wmem         "${TCP_WMIN} ${TCP_WDEF} ${TCP_WMAX}"
apply net.ipv4.tcp_mem          "${TCP_MEM_MIN} ${TCP_MEM_PRESSURE} ${TCP_MEM_MAX}"

echo -e "\n${BOLD}── UDP буферы (WireGuard) ───────────────────────────${NC}"
# WireGuard работает поверх UDP — TCP-буферы на него не влияют
apply net.ipv4.udp_rmem_min     "$UDP_RMIN"
apply net.ipv4.udp_wmem_min     "$UDP_RMIN"
apply net.core.optmem_max       "$UDP_RDEF"

echo -e "\n${BOLD}── Congestion control ───────────────────────────────${NC}"
apply net.core.default_qdisc fq
if [[ $BBR_OK -eq 1 ]]; then
    apply net.ipv4.tcp_congestion_control bbr
else
    apply net.ipv4.tcp_congestion_control cubic
fi

echo -e "\n${BOLD}── Очереди ──────────────────────────────────────────${NC}"
apply net.core.netdev_max_backlog  "$NETDEV_BACKLOG"
apply net.core.somaxconn           "$SOMAXCONN"
apply net.ipv4.tcp_max_syn_backlog "$BACKLOG"

echo -e "\n${BOLD}── TIME_WAIT ─────────────────────────────────────────${NC}"
apply net.ipv4.tcp_max_tw_buckets "$TW_BUCKETS"
# H-1 fix: tcp_tw_reuse=0 — значение 1 ломает NAT/MASQUERADE на роутере:
# переиспользованный порт может получить RST от удалённого хоста у которого
# ещё есть состояние для старого соединения с тем же 4-tuple.
apply net.ipv4.tcp_tw_reuse       0
apply net.ipv4.tcp_fin_timeout    15
# tcp_tw_recycle удалён в 4.12 — на Ubuntu 22.04 (5.15+) всегда пропускаем
skip "net.ipv4.tcp_tw_recycle — удалён в ядре 4.12+ (целевая платформа: Ubuntu 22.04+)"

echo -e "\n${BOLD}── TCP фичи ─────────────────────────────────────────${NC}"
# H-2 fix: tcp_fastopen=1 (только клиент) вместо 3 (клиент+сервер).
# fastopen=3 на роутере с ip_forward=1 позволяет амплификацию:
# SYN с поддельным src → роутер отвечает SYN-ACK+данные на чужой адрес.
apply net.ipv4.tcp_fastopen           1
apply net.ipv4.tcp_mtu_probing        1
apply net.ipv4.tcp_slow_start_after_idle 0
apply net.ipv4.tcp_sack               1
apply net.ipv4.tcp_dsack              1
apply net.ipv4.tcp_timestamps         1
apply net.ipv4.tcp_window_scaling     1
# M-4 fix: tcp_no_metrics_save убран.
# Значение 1 запрещает кэшировать RTT/SSTHRESH/PMTU между соединениями.
# При включённом BBR это контрпродуктивно: BBR сам управляет окном,
# но PMTU-кэш нужен для правильного выбора MSS.
# H-3 fix: tcp_ecn=2 вместо 1.
# Значение 1 инициирует ECN переговоры — часть провайдеров/файрволов
# дропает SYN с ECN-битами → тихие обрывы. Значение 2: принимать ECN
# если партнёр предлагает, но не инициировать самому.
apply net.ipv4.tcp_ecn                2
# M-3 fix: 300s вместо 60s.
# 60s слишком агрессивно system-wide: каждое idle TCP-соединение (включая
# management, SSH) начинает keepalive через 1 минуту → лишний трафик на роутере.
# WireGuard управляет keepalive на уровне UDP самостоятельно.
apply net.ipv4.tcp_keepalive_time     300
apply net.ipv4.tcp_keepalive_intvl    10
apply net.ipv4.tcp_keepalive_probes   6
# tcp_low_latency и tcp_fack удалены в 4.14/4.15 — на Ubuntu 22.04 всегда пропускаем
skip "net.ipv4.tcp_low_latency — удалён в ядре 4.14+"
skip "net.ipv4.tcp_fack — удалён в ядре 4.15+ (поглощён SACK)"

echo -e "\n${BOLD}── Защита (SYN-флуд, orphans) ───────────────────────${NC}"
apply net.ipv4.tcp_syncookies    1
apply net.ipv4.tcp_max_orphans   "$MAX_ORPHANS"
apply net.ipv4.tcp_syn_retries   3
apply net.ipv4.tcp_synack_retries 3

echo -e "\n${BOLD}── Forwarding + защита от спуфинга (VPN) ────────────${NC}"
apply net.ipv4.ip_forward 1
apply_if_exists net.ipv6.conf.all.forwarding 1
# L-5: rp_filter — защита от IP-спуфинга через forwarding path.
# Используем режим 2 (loose): пакет дропается только если source IP вообще
# не маршрутизируется ни через какой интерфейс. Режим 1 (strict) требует
# симметричного маршрута source→router, что ломает WireGuard и любой VPN:
# входящие туннельные пакеты приходят на wg-интерфейс, а их src
# маршрутизируется через eth0 → strict расценивает это как спуфинг и дропает.
# conf.default применяется ко всем новым интерфейсам (включая wg0, wg10...).
apply net.ipv4.conf.all.rp_filter     2
apply net.ipv4.conf.default.rp_filter 2
# L-5: ICMP redirects отключить на роутере — иначе возможно отравление
# таблицы маршрутов через поддельные ICMP redirect сообщения.
apply net.ipv4.conf.all.accept_redirects   0
apply net.ipv4.conf.all.send_redirects     0
apply net.ipv4.conf.all.accept_source_route 0

echo -e "\n${BOLD}── VM (меньше свопинга) ─────────────────────────────${NC}"
apply vm.swappiness          10
apply vm.vfs_cache_pressure  50

# ── C-2 fix: сохраняем файл ДО цикла tc ─────────────────────────────────────
# Порядок гарантирует: даже если tc упадёт, sysctl.d файл уже сохранён
# и настройки применятся после перезагрузки.
SYSCTL_FILE="/etc/sysctl.d/99-cascade-tuning.conf"
echo -e "\n${BOLD}── Сохранение в ${SYSCTL_FILE} ───────────────────────${NC}"

# L-3: бэкап перед перезаписью
if [[ -f "$SYSCTL_FILE" ]]; then
    cp "$SYSCTL_FILE" "${SYSCTL_FILE}.bak.$(date +%s)"
    info "Бэкап предыдущего конфига сохранён"
fi

cat > "$SYSCTL_FILE" << EOF
# Auto-generated by tcp_tune.sh
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

# ── UDP буферы (WireGuard) ────────────────────────────────────────────────
net.ipv4.udp_rmem_min         = $UDP_RMIN
net.ipv4.udp_wmem_min         = $UDP_RMIN
net.core.optmem_max           = $UDP_RDEF

# ── Congestion control ────────────────────────────────────────────────────
net.core.default_qdisc        = fq
net.ipv4.tcp_congestion_control = $( [[ $BBR_OK -eq 1 ]] && echo bbr || echo cubic )

# ── Очереди ───────────────────────────────────────────────────────────────
net.core.netdev_max_backlog   = $NETDEV_BACKLOG
net.core.somaxconn            = $SOMAXCONN
net.ipv4.tcp_max_syn_backlog  = $BACKLOG

# ── TIME_WAIT ─────────────────────────────────────────────────────────────
net.ipv4.tcp_max_tw_buckets   = $TW_BUCKETS
# tw_reuse=0: значение 1 ломает NAT/MASQUERADE на роутере
net.ipv4.tcp_tw_reuse         = 0
net.ipv4.tcp_fin_timeout      = 15

# ── TCP фичи ──────────────────────────────────────────────────────────────
# fastopen=1: только клиент; =3 на роутере с ip_forward — вектор амплификации
net.ipv4.tcp_fastopen         = 1
net.ipv4.tcp_mtu_probing      = 1
net.ipv4.tcp_slow_start_after_idle = 0
net.ipv4.tcp_sack             = 1
net.ipv4.tcp_dsack            = 1
net.ipv4.tcp_timestamps       = 1
net.ipv4.tcp_window_scaling   = 1
# ecn=2: принимать ECN если партнёр предлагает, не инициировать самому
# (=1 инициирует — часть провайдеров дропает SYN с ECN-битами)
net.ipv4.tcp_ecn              = 2
# keepalive=300s: 60s слишком агрессивно system-wide для роутера
net.ipv4.tcp_keepalive_time   = 300
net.ipv4.tcp_keepalive_intvl  = 10
net.ipv4.tcp_keepalive_probes = 6

# ── Защита ────────────────────────────────────────────────────────────────
net.ipv4.tcp_syncookies       = 1
net.ipv4.tcp_max_orphans      = $MAX_ORPHANS
net.ipv4.tcp_syn_retries      = 3
net.ipv4.tcp_synack_retries   = 3

# ── VPN forwarding + защита от спуфинга ───────────────────────────────────
net.ipv4.ip_forward                 = 1
net.ipv6.conf.all.forwarding        = 1
# rp_filter=2 (loose): дропать только если src вообще не маршрутизируется.
# rp_filter=1 (strict) ломает VPN: src туннельного пакета маршрутизируется
# через другой интерфейс → strict считает это спуфингом и дропает.
net.ipv4.conf.all.rp_filter         = 2
net.ipv4.conf.default.rp_filter     = 2
net.ipv4.conf.all.accept_redirects  = 0
net.ipv4.conf.all.send_redirects    = 0
net.ipv4.conf.all.accept_source_route = 0

# ── VM ────────────────────────────────────────────────────────────────────
vm.swappiness                 = 10
vm.vfs_cache_pressure         = 50

# NOTE: следующие параметры удалены из ядра и НЕ применяются:
# net.ipv4.tcp_low_latency  (удалён в 4.14)
# net.ipv4.tcp_fack         (удалён в 4.15, поглощён SACK)
# net.ipv4.tcp_tw_recycle   (удалён в 4.12)
# net.ipv4.tcp_no_metrics_save не включён: контрпродуктивен с BBR
EOF

ok "Сохранено → ${SYSCTL_FILE}"

# ── fq на физические интерфейсы ─────────────────────────────────────────────
# C-2 fix: этот блок ПОСЛЕ записи файла — сбой tc не ломает персистентность
# M-1 + L-4 fix: glob вместо ls; пропускаем WG/GRE/tunnel pseudo-интерфейсы
#
# Почему wg*/awg* пропускаются здесь (но fq у них всё равно будет):
# net.core.default_qdisc=fq (строка выше) применяется к НОВЫМ интерфейсам.
# WireGuard-интерфейсы создаются пользователем в UI — уже после применения
# default_qdisc → автоматически получают fq при создании. Явный tc replace
# для них избыточен. Физические интерфейсы (eth0/eth1) существуют с момента
# загрузки ОС — до этого скрипта — и требуют явного replace.
#
# fq на wg-интерфейсах (как egress транзитного туннеля) даёт честность между
# потоками клиентов ДО шифрования. Это полезно в каскадной топологии:
# Client→wg10→wg11(fq)→encrypt→eth0. Просто происходит автоматически.
echo -e "\n${BOLD}── qdisc fq на физических интерфейсах ───────────────${NC}"
for iface_path in /sys/class/net/*/; do
    iface="${iface_path%/}"
    iface="${iface##*/}"
    # Пропускаем loopback
    [[ "$iface" == "lo" ]] && continue
    # wg*/awg*: fq применяется автоматически через default_qdisc при создании
    # интерфейса — явный replace здесь не нужен (см. комментарий выше)
    case "$iface" in
        wg*|awg*|tun*|tap*|gre*|sit*|ip6tnl*|dummy*) continue ;;
        # Docker/veth: qdisc сбрасывается daemon'ом при каждом перезапуске контейнера
        veth*|br*|docker*) continue ;;
    esac
    if tc qdisc replace dev "$iface" root fq 2>/dev/null; then
        ok "fq → $iface"
    else
        warn "Не удалось применить fq на $iface (возможно нет поддержки qdisc)"
    fi
done

# ── итог ───────────────────────────────────────────────────────────────────
echo -e "\n${GREEN}${BOLD}══ Готово ══════════════════════════════════════════${NC}"
printf "  %-16s %s\n" "RAM:"     "${RAM_MB} MB → tier: ${TIER}"
printf "  %-16s %s\n" "rmem:"    "$(( CORE_RMAX / 1024 / 1024 )) MB max"
printf "  %-16s %s\n" "wmem:"    "$(( CORE_WMAX / 1024 / 1024 )) MB max"
printf "  %-16s %s\n" "udp_buf:" "$(( UDP_RMAX / 1024 / 1024 )) MB max"
printf "  %-16s %s\n" "tcp_mem:" "$(( TCP_MEM_MAX * 4 / 1024 )) MB max (${TCP_MEM_MAX} pages)"
printf "  %-16s %s\n" "BBR:"     "$( [[ $BBR_OK -eq 1 ]] && echo enabled || echo 'cubic fallback' )"
printf "  %-16s %s\n" "Config:"  "${SYSCTL_FILE}"
