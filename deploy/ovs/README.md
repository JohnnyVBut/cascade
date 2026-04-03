# Cascade — Isolated Network Mode (OVS)

Use this setup when running Cascade on a home/lab Docker host where:
- The host is connected via 802.1q VLAN trunk
- Containers are connected to specific VLANs via Open vSwitch (`ovs-docker`)
- `--network host` is undesirable (isolation required)

Inter-VLAN routing and access policies are handled by an external firewall.

---

## How it works

```
Host (802.1q trunk on eth0)
  └── OVS bridge br-trunk
        └── VLAN 20 port → cascade container eth0 (192.168.20.5/24)
              └── wg10 (10.100.0.1/24)  ← WireGuard netns inside container
              └── wg11 (10.100.1.1/24)
```

1. Container starts with `network_mode: none` (only loopback).
   `entrypoint.sh` waits up to `NETWORK_WAIT_TIMEOUT` seconds for a default route.

2. `ovs-docker add-port` attaches a VLAN-tagged interface with IP + gateway.
   The container's default route appears → `entrypoint.sh` proceeds → Cascade starts.

3. WireGuard interfaces (`wg10`, `wg11` …) are created inside the container's
   network namespace. `iptables`, `ip route`, `ip rule` all operate in that netns —
   fully isolated from the host.

---

## Prerequisites

```bash
apt install openvswitch-switch
```

Verify OVS is running:
```bash
ovs-vsctl show
```

---

## Deploy

### 1. Configure `deploy/.env`

```bash
cp deploy/.env.example deploy/.env   # or create manually
# Required:
WG_HOST=192.168.20.5   # IP that OVS will assign to the container
PORT=8888
PASSWORD_HASH='$2b$...'

# OVS connection parameters (read by attach.sh):
OVS_BRIDGE=br-trunk
OVS_IP=192.168.20.5/24
OVS_GATEWAY=192.168.20.1
OVS_VLAN=20
# OVS_MAC=02:42:ac:14:00:05  (optional — fixes MAC to avoid ARP churn on restart)
# OVS_IFACE=eth0  (default)
# CASCADE_CONTAINER=cascade  (default)
```

### 2. Start the container (no network yet)

```bash
docker compose -f docker-compose.isolated.yml up -d
docker logs -f cascade   # will show "Waiting for default route..."
```

### 3. Attach OVS interface

```bash
bash deploy/ovs/attach.sh
# All values read from deploy/.env — no prompts needed if configured.
```

Or pass args explicitly:
```bash
bash deploy/ovs/attach.sh \
  --bridge br-trunk \
  --ip 192.168.20.5/24 \
  --gateway 192.168.20.1 \
  --vlan 20
```

Cascade starts within seconds after the route appears.
Web UI: `http://192.168.20.5:8888`

### 4. Detach (e.g. before stopping)

```bash
bash deploy/ovs/detach.sh
docker compose -f docker-compose.isolated.yml down
```

---

## Restart behaviour

On container restart (`restart: unless-stopped`), the container starts with
`network_mode: none` again and waits for OVS. If OVS is configured to
automatically re-attach ports on container restart (e.g. via a systemd hook
or Netplan OVS integration), Cascade will come up fully automatically.

### Automatic re-attach on restart (systemd example)

Create `/etc/systemd/system/cascade-ovs-attach.service`:

```ini
[Unit]
Description=Attach OVS port to Cascade container
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/bash /path/to/cascade/deploy/ovs/attach.sh
ExecStop=/usr/bin/bash /path/to/cascade/deploy/ovs/detach.sh

[Install]
WantedBy=multi-user.target
```

```bash
systemctl enable --now cascade-ovs-attach.service
```

---

## Differences from host-network mode

| | Host mode (`docker-compose.go.yml`) | Isolated mode (`docker-compose.isolated.yml`) |
|---|---|---|
| `network_mode` | `host` | `none` (OVS attaches after start) |
| WireGuard netns | Host | Container (isolated) |
| `iptables` scope | Host | Container only |
| `ip route` scope | Host | Container only |
| Ports | Direct (no mapping) | Direct via OVS IP |
| `/etc/hostname` | Mounted from host | Container `hostname:` field |
| `ip_forward` | Set on host by `entrypoint.sh` | Set in container netns by `entrypoint.sh` |
