# ============================================================
# Cascade — Go/Fiber build
# ============================================================
# Stage 1: Build Go binary
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy source first (go mod tidy needs imports to resolve indirect deps).
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Resolve full dependency graph, update go.mod with indirect deps, generate go.sum.
# Cached unless go.mod or source changes.
RUN go mod tidy

# Build static binary.
# CGO_ENABLED=0: fully static binary, no libc dependency.
# -ldflags="-s -w": strip debug symbols → smaller binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o cascade \
    ./cmd/awg-easy

# ============================================================
# Stage 2: Runtime image
# Base: amneziawg-go (has awg-quick, awg, wg-quick, wg tools)
# ============================================================
FROM amneziavpn/amneziawg-go:latest

HEALTHCHECK --interval=1m --timeout=5s --retries=3 \
    CMD /usr/bin/timeout 5s /bin/sh -c "/usr/bin/wg show | /bin/grep -q interface || exit 1"

# Switch to Yandex mirror (faster from RU/CIS)
RUN sed -i 's|https://dl-cdn.alpinelinux.org|https://mirror.yandex.ru/mirrors|g' /etc/apk/repositories

# Runtime dependencies:
# - dumb-init: proper PID 1 signal handling
# - iptables / iptables-legacy: firewall management
# - iproute2: ip route/rule commands
# - ipset: alias ipsets for firewall rules
# NOTE: no node, no libstdc++, no libgcc — Go binary is static
RUN apk add --no-cache \
    dumb-init \
    iptables \
    iptables-legacy \
    iproute2 \
    ipset

# Use iptables-legacy as default iptables.
# Alpine не имеет update-alternatives (это команда dpkg/Debian).
RUN ln -sf /sbin/iptables-legacy         /sbin/iptables && \
    ln -sf /sbin/iptables-legacy-restore /sbin/iptables-restore && \
    ln -sf /sbin/iptables-legacy-save    /sbin/iptables-save

# Copy the static Go binary from build stage
COPY --from=builder /app/cascade /usr/local/bin/cascade

# Data directory (mapped via volume in docker-compose)
RUN mkdir -p /etc/wireguard/data

CMD ["/usr/bin/dumb-init", "cascade", "--data-dir", "/etc/wireguard/data"]
