# Cascade — User Manual

## Table of Contents

1. [First Login and Setup](#1-first-login-and-setup)
2. [WireGuard Interfaces](#2-wireguard-interfaces)
3. [AWG 2.0 — Obfuscation Templates](#3-awg-20--obfuscation-templates)
4. [Client Peers](#4-client-peers)
5. [S2S Interconnect (Server-to-Server Tunnels)](#5-s2s-interconnect-server-to-server-tunnels)
6. [Gateways](#6-gateways)
7. [Routing](#7-routing)
8. [NAT](#8-nat)
9. [Firewall: Aliases](#9-firewall-aliases)
10. [Firewall: Rules and PBR](#10-firewall-rules-and-pbr)
11. [Global Settings](#11-global-settings)
12. [Administration](#12-administration)

---

## 1. First Login and Setup

### First Run

On first container start you will see the **Welcome to Cascade** screen with a first-run setup form:

- **Username** — admin username (default: `admin`)
- **Password** — minimum 8 characters
- **Confirm Password**

After creating the account you will be redirected to the login page.

### Login

Enter your username and password. If TOTP two-factor authentication is enabled for your account, a second screen will ask for the 6-digit code from your authenticator app.

### Interface Overview

The left sidebar contains the following sections:

| Section | Purpose |
|---------|---------|
| **Interfaces** | Manage WireGuard/AWG interfaces and peers |
| **Gateways** | Gateways and health monitoring |
| **Routing** | Routing tables and static routes |
| **NAT** | Source NAT / MASQUERADE rules |
| **Firewall → Aliases** | Named sets of addresses and ports |
| **Firewall → Rules** | Packet filtering and Policy-Based Routing |
| **Settings** | Global settings and AWG2 templates |
| **Administration** | Users, API tokens, TOTP, backup |

---

## 2. WireGuard Interfaces

### What Is an Interface

Each WireGuard interface is an independent VPN tunnel with its own key pair, address, and port. You can create multiple interfaces for different purposes: one for clients, another for a site-to-site connection with a remote server.

### Creating an Interface

Click **"+ New Interface"** on the Interfaces page.

#### Manual Mode

| Field | Description |
|-------|-------------|
| **Interface Name** | Human-readable name (optional) |
| **Protocol** | `WireGuard 1.0` or `AmneziaWG 2.0` |
| **Tunnel Address** | Interface IP in CIDR notation (e.g. `10.100.0.1/24`) |
| **Listen Port** | UDP port (auto-selected from portPool, or set manually) |
| **Disable Routes** | Disable automatic kernel route injection. Enable for S2S interconnect interfaces — you manage routes manually |
| **Disable NAT** | Do not create an automatic MASQUERADE rule in PostUp. Use when managing NAT manually via the NAT section |

When **AmneziaWG 2.0** is selected, an **Obfuscation Parameters** section appears — see [section 3](#3-awg-20--obfuscation-templates).

### Managing an Interface

Each interface appears on its own tab. The interface card provides the following buttons:

- **Start / Stop** — bring the interface up or down
- **Restart** — restart without changing configuration
- **Edit** — modify name, address, port, protocol, and AWG2 parameters
- **Export My Params** — download a JSON file with the public key and endpoint to share with an S2S partner
- **Backup / Restore** — save or restore the interface configuration along with all its peers

### Choosing a Protocol

| | WireGuard 1.0 | AmneziaWG 2.0 |
|--|---|---|
| Compatibility | Any WireGuard client | AmneziaWG clients only |
| Obfuscation | None | Yes — protocol imitation (QUIC, TLS, DNS, etc.) |
| Use case | Home networks, no censorship | Countries with deep packet inspection (DPI) |

---

## 3. AWG 2.0 — Obfuscation Templates

### Why Obfuscation Matters

AmneziaWG modifies the characteristics of the initial handshake packets so that DPI equipment cannot identify WireGuard traffic. Each set of parameters is called a **template** and is applied when creating an interface.

### Template Parameters

| Group | Parameters | Purpose |
|-------|-----------|---------|
| **Jitter** | Jc, Jmin, Jmax | Number and size of jitter packets |
| **Size** | S1, S2, S3, S4 | Sizes of service packets |
| **Headers** | H1, H2, H3, H4 | Magic-bytes header ranges |
| **Imitation** | I1–I5 | Protocol imitation templates |

### Creating a Template Manually

1. Go to **Settings → AWG2 Templates**
2. Click **"+ New Template"**
3. Enter the parameter values or click **⚡ Generate** for auto-generation

### Profile Generator (⚡ Generate)

The generator creates the I1 parameter, which imitates a packet from a real protocol:

| Profile | What It Imitates |
|---------|-----------------|
| **Random** | A random non-composite profile |
| **QUIC Initial** | QUIC Initial packet (RFC 9000) |
| **QUIC 0-RTT** | QUIC session resumption |
| **TLS 1.3** | TLS ClientHello |
| **DTLS 1.2** | DTLS ClientHello (WebRTC, VoIP) |
| **HTTP/3** | HTTP/3 over QUIC |
| **SIP** | SIP REGISTER request |
| **Noise_IK (WireGuard)** | WireGuard Noise_IK handshake |
| **DNS Query (RFC 1035)** | DNS A/AAAA query |
| **TLS→QUIC (composite)** | TLS ClientHello followed by QUIC Initial |
| **QUIC Burst (composite)** | QUIC Initial + 0-RTT + HTTP/3 |

Additional generator options:

- **Intensity**: `low` / `medium` / `high` — affects packet sizes and Jmax
- **Host (SNI)**: domain to embed in the I1 packet (leave empty for a random one from the pool)
- **Browser Fingerprint**: tailors packet sizes to a specific browser (not available for SIP and DNS Query)

### Applying a Template

- When creating an interface, select a template from the **Obfuscation Profile** dropdown
- Or click **⚡** next to the field to generate parameters on the fly without saving

> **Important:** H1–H4 ranges must be **identical** on both sides of the tunnel. If you change a template on one server, you must update all connected clients as well.

---

## 4. Client Peers

### Creating a Peer

On the interface tab, click **"+ New Peer"** (quick create with just a name) or **"Manual"** (full form).

#### Creation Form Fields

| Field | Description |
|-------|-------------|
| **Name** | Peer name (e.g. "Ivan's Laptop") |
| **Peer Type** | `Client` — standard VPN client |
| **Key Mode** | **Generate Keys** — server generates the key pair; **Enter Manually** — you provide the client's public key |
| **Allowed IPs** | Peer's tunnel IP address (auto-assigned /32 if left empty) |
| **Client Allowed IPs** | Routes pushed to the client in its config. Default `0.0.0.0/0, ::/0` routes all traffic through the VPN |
| **Endpoint** | Client's IP:port (optional, usually not needed for clients) |
| **Persistent Keepalive** | Keepalive interval in seconds (25 is recommended for clients behind NAT) |

### Distributing Configuration to Clients

After creating a peer with **Generate Keys**:

- **QR Code** — scan with the AmneziaWG app (iOS / Android)
- **Download Config** — download the `.conf` file for manual import

> The private key is stored on the server and is only available while the peer exists. Once a peer is deleted, the key cannot be recovered.

### Editing a Peer

Click the pencil icon on the peer card. A modal opens with the following fields:

- **Name** — change the display name
- **Client Allowed IPs** — change routes pushed to the client
- **Persistent Keepalive** — change the keepalive interval

### Enabling / Disabling a Peer

Use the toggle on the peer card. A disabled peer is excluded from the WireGuard configuration — no traffic passes.

### Statistics

The peer card displays:

- **Online** — blinking red dot = peer transferred data within the last ~3 minutes
- **Endpoint** — the client's current IP address (updated every second)
- **RX / TX** — session traffic and total lifetime traffic

---

## 5. S2S Interconnect (Server-to-Server Tunnels)

### Purpose

S2S Interconnect connects two Cascade routers into a unified network. Use cases:

- Joining office networks
- Cascaded VPN (client traffic passes through a chain of servers)
- Channel redundancy

### WireGuard Routing Limitations

WireGuard uses `allowedIPs` as its routing table. When there are **multiple** S2S peers on one interface with the same prefix:

- `0.0.0.0/0` can only be assigned to **one** peer — otherwise WireGuard picks arbitrarily
- Recommended: use specific subnets (`10.200.0.0/24`) for each peer

### Step-by-Step S2S Setup

Example: Server A ↔ Server B, tunnel interface addresses `10.100.0.1/30` and `10.100.0.2/30`.

> `/30` is the standard choice for a point-to-point tunnel (4 addresses, 2 usable). Use `/24` or wider only if you plan to have a full client subnet behind the interface.

#### Step 1 — Server A: Create an Interface

1. **Interfaces → + New Interface**
2. Protocol: choose as needed
3. Address: `10.100.0.1/30`
4. **Disable Routes: ✓** (WireGuard will not touch the routing table)
5. Save and click **Start**

#### Step 2 — Server A: Export Parameters

1. Click **"Export My Params"** on the interface card
2. A file `wg10-params.json` downloads containing:
   - `publicKey` — Server A's public key
   - `endpoint` — Server A's external IP:port
   - `address` — Server A's interface address
   - `protocol` — the protocol in use

Share this file with Server B's administrator.

#### Step 3 — Server B: Create an Interface and Import

1. Create an interface on Server B (Address: `10.100.0.2/30`, Disable Routes: ✓)
2. Click **"Import JSON"** on the interface peers page
3. Upload the file from Server A
4. The system automatically creates an Interconnect peer with Server A's parameters
5. A PSK (Pre-Shared Key) is generated automatically

#### Step 4 — Server B: Export Reply Parameters

1. Click **"Export My Params"** on Server B's interface
2. The JSON will include a `presharedKey` — already known only to Server B
3. Share this file with Server A's administrator

#### Step 5 — Server A: Import Server B's Parameters

1. **Import JSON** → upload the file from Server B
2. The PSK is synchronized automatically
3. The tunnel is ready — both servers can reach each other at `10.100.0.1` and `10.100.0.2`

### Static Routes for Additional Subnets

Once the tunnel is up, each server automatically knows the connected subnet of its own interface (`10.100.0.0/30`). Static routes are only needed when you want to reach **other subnets behind the remote router** — for example, its client interface subnet.

**Example:** Server A has a client interface `wg11` with subnet `10.8.0.0/24`. For Server B to reach Server A's clients:

- **Server B → Routing → Static Routes → + Add**
  - Destination: `10.8.0.0/24`
  - Via: `10.100.0.1` (Server A's tunnel IP)
  - Dev: `wg10`

Repeat symmetrically if Server B also has a client interface.

### Editing an S2S Peer

Click the pencil icon on the peer card. Available fields:

- **Endpoint** — change the remote server's IP:port (applied without restarting the tunnel)
- **Allowed IPs** — change the routed subnets
- **Persistent Keepalive** — maintain the connection through NAT

---

## 6. Gateways

### Purpose

Gateways are outbound routes (ISPs, upstream VPNs) that are actively monitored. They are used together with Firewall Rules for **Policy-Based Routing** — directing specific traffic through a particular provider.

### Creating a Gateway

**Gateways → + Add Gateway**

| Field | Description |
|-------|-------------|
| **Name** | Gateway name (e.g. "KZ ISP") |
| **Interface** | Host network interface (eth0, wg10, etc.) |
| **Gateway IP** | Next-hop IP address |
| **Monitor Address** | Address for ICMP pings (defaults to Gateway IP) |
| **Monitor Interval** | Ping interval in seconds |
| **Latency Threshold** | Latency above which the gateway is "Degraded" |

#### HTTP Probe (Optional)

Use this when ICMP is blocked:

| Field | Description |
|-------|-------------|
| **HTTP URL** | URL to check (e.g. `https://example.com`) |
| **Expected Status** | Expected HTTP status code (200, 204, etc.) |
| **Interval** | Check interval (minimum 10 seconds) |

### Gateway Statuses

| Status | Meaning |
|--------|---------|
| 🟢 **Healthy** | Loss below degraded threshold |
| 🟡 **Degraded** | Loss above degraded threshold but below down threshold |
| 🔴 **Down** | Loss exceeds threshold or no response |

Thresholds are configured in **Settings → Gateway Healthy/Degraded Threshold**.

### Gateway Groups

Combine multiple gateways with automatic failover.

**Gateways → + Add Group**

| Field | Description |
|-------|-------------|
| **Name** | Group name |
| **Trigger** | Failover criterion: `packetloss` / `latency` / `packetloss_latency` |
| **Members** | List of gateways with priority tiers (tier 1 = primary, tier 2 = backup) |

When a tier-1 gateway degrades, traffic is automatically switched to tier-2.

### Fallback When a Gateway Is Down

In a firewall rule bound to a gateway, enable **Fallback to Default**:

- When status is "Down", traffic is routed through the system default gateway
- 30 seconds after the gateway recovers, routing returns to it

---

## 7. Routing

### Status — Kernel Routing Table

**Routing → Status** — displays the current routing table from the Linux kernel (equivalent to `ip route show`).

Columns: protocol, destination, via (gateway), interface, metric.

**Route Test:**

Enter **Dst** (destination IP) and optionally **Src** (source IP).

- Without Src: shows the route from the kernel routing table
- With Src: runs a Policy-Based Routing trace — which firewall rule will match and through which gateway the traffic will flow

Result: `matched route`, `matchedRule` (PBR rule), `steps` (trace steps).

### Routing Tables

**Routing → Tables** — list of routing tables discovered in the kernel (from `ip rule show`).

When PBR is configured, each firewall rule gets a dedicated routing table with a `default via <gateway>` entry.

### Static Routes

**Routing → Static → + Add Route**

| Field | Description |
|-------|-------------|
| **Destination** | CIDR or `default` (required) |
| **Via** | Gateway IP address |
| **Dev** | Interface name (optional) |
| **Metric** | Route priority (lower = higher priority) |
| **Table** | Routing table (default: `main`) |
| **Description** | Comment |

The **Enabled** toggle enables or disables the route without deleting it.

> Static routes are automatically restored after a container restart.

---

## 8. NAT

### Purpose

NAT (Network Address Translation) replaces the source IP of packets as they leave through an interface. It is required for VPN clients to access the internet.

> When a client interface is created, a MASQUERADE rule is added **automatically**. The NAT section is for fine-grained control.

### NAT Rules

**NAT → + Add Rule**

| Field | Description |
|-------|-------------|
| **Name** | Rule name |
| **Source** | `any` / CIDR subnet / IP / alias |
| **Out Interface** | Outgoing interface (eth0, wg10, etc.) |
| **Type** | `MASQUERADE` — replace src with the interface IP; `SNAT` — replace src with a fixed IP |
| **To Source** | For type=SNAT — the target IP |
| **Comment** | Comment |

### Auto-Rules from Interfaces

The NAT page displays rules automatically created by WireGuard interfaces (marked with an icon). They are added in PostUp and provide basic NAT for clients.

### Source Alias in NAT

When the source type is **Alias**, the rule applies to all addresses in the alias. Particularly useful for ipset aliases with thousands of IP addresses (e.g. all IPs of a country).

---

## 9. Firewall: Aliases

Aliases are named sets of addresses or ports. They are used in firewall rules and NAT instead of manually entering addresses each time.

### Alias Types

| Type | Description | Example Entries |
|------|-------------|-----------------|
| **host** | Individual IP addresses | `192.168.1.1`, `10.0.0.5` |
| **network** | CIDR subnets | `10.0.0.0/8`, `192.168.0.0/16` |
| **ipset** | Large IP sets (kernel ipset) | — loaded from file or generated |
| **group** | Combination of host/network aliases | — select from existing aliases |
| **port** | Ports and ranges | `80`, `443`, `8080-8090` |
| **port-group** | Combination of port aliases | — select from existing port aliases |

### Creating an Alias

**Firewall → Aliases → + Add**

1. Select the type
2. Enter entries (one per line)
3. For group/port-group — select members from the list

### Generating an ipset from RIPE (Countries and AS Numbers)

For **ipset** type aliases:

1. In the **Generate** section, select the source:
   - **Country** — enter a country code (RU, US, CN, etc.)
   - **ASN** — enter an autonomous system number
   - **ASN List** — multiple AS numbers separated by commas
2. Click **Generate**
3. The system downloads current prefixes from RIPE NCC and populates the ipset

### Uploading from a File

For ipset aliases, click **Upload** (in the edit modal) — upload a text file with CIDR prefixes, one per line.

---

## 10. Firewall: Rules and PBR

### Rule Evaluation Order

Rules are evaluated **top to bottom** — the first match applies. Use the **↑ / ↓** buttons on each rule to adjust the order.

### Creating a Rule

**Firewall → Rules → + Add Rule**

| Field | Description |
|-------|-------------|
| **Name** | Rule name |
| **Interface** | `any` or a specific interface (wg10, eth0, etc.) |
| **Protocol** | `any` / `tcp` / `udp` / `icmp` |
| **Source** | `any` / IP / subnet / alias; optionally with port |
| **Destination** | `any` / IP / subnet / alias; optionally with port |
| **Action** | `Accept` / `Drop` / `Reject` |
| **Gateway** | (optional) — for PBR: the gateway to route matching traffic through |
| **Fallback to Default** | If the gateway goes down, route traffic through the system default |

### Policy-Based Routing (PBR)

PBR routes traffic through a specific gateway based on connection characteristics (source, destination, protocol, port) — independently of the kernel routing table.

**How It Works:**

1. Create a gateway in the Gateways section
2. In a firewall rule, set **Action = Accept** and select a **Gateway**
3. The system creates:
   - `ip route table N default via <gateway_ip>`
   - `ip rule add fwmark N lookup N`
   - `iptables mangle MARK --set-mark N` for matching traffic

**Example:** Route traffic from `10.8.0.0/24` through the "KZ Provider" gateway:

1. Create a gateway "KZ Provider" (interface: eth1, gateway IP: 10.0.0.1)
2. **Firewall → Rules → + Add**:
   - Source: `10.8.0.0/24`
   - Action: Accept
   - Gateway: KZ Provider

**Example with an alias:** Route traffic to Kazakh IPs through a Kazakh ISP:

1. Create an ipset alias "kz_prefixes" using Generate → Country: KZ
2. **Firewall → Rules → + Add**:
   - Destination: alias `kz_prefixes`
   - Action: Accept
   - Gateway: KZ Provider

### Testing PBR

**Routing → Status → Route Test**:
- Dst: destination IP
- Src: client IP

The result shows which gateway that client's traffic to that destination will use.

### Default Firewall Policy

At the bottom of the **Firewall → Rules** page is the **Default Policy** card:

- **Accept** (default) — traffic that does not match any rule is allowed
- **Drop** — traffic that does not match any rule is silently discarded

> When switching to **Drop**, the system will ask for confirmation. Make sure you have explicit rules permitting the required traffic — otherwise connections may be interrupted.

---

## 11. Global Settings

**Settings → Global Settings**

### Router Identity

| Field | Description |
|-------|-------------|
| **Router Name** | Display name for the router |
| **Public IP Mode** | `auto` — auto-detect; `manual` — enter manually |
| **Public IP** | When mode=manual — the server's external IP |

### VPN Settings

| Field | Description |
|-------|-------------|
| **DNS** | DNS servers for client configs (comma-separated) |
| **Default Persistent Keepalive** | Default keepalive for new peers (seconds) |
| **Default Client Allowed IPs** | Default AllowedIPs for client configs |

### Address and Port Pools

| Field | Description | Example |
|-------|-------------|---------|
| **Subnet Pool** | Range for auto-assigning interface addresses | `192.168.0.0/16` |
| **Port Pool** | UDP port range for new interfaces | `51831-65535` |

### Gateway Monitoring

| Field | Description |
|-------|-------------|
| **Gateway Window** | Sliding window for statistics calculation (seconds) |
| **Healthy Threshold** | Minimum % of successful checks for Healthy status |
| **Degraded Threshold** | Minimum % for Degraded status |

### Firewall

| Field | Description |
|-------|-------------|
| **Default Firewall Policy** | `accept` or `drop` — policy for unmatched traffic |

---

## 12. Administration

### User Management

**Administration → Users**

- Create additional users
- Reset passwords
- Enable TOTP two-factor authentication

### TOTP (2FA)

1. Click **"Setup TOTP"** next to a user
2. Scan the QR code with Google Authenticator / Authy / any TOTP app
3. After activation, a 6-digit code will be required at every login

### API Tokens

**Administration → API Tokens**

Tokens for API access without a session. Used for automation, scripts, and CI/CD.

- Click **"+ New Token"**, set a name
- Copy the token — it is shown only once
- Pass it in the header: `Authorization: Bearer <token>`

### Backup and Restore

#### Interface Backup

Click **Backup** on the interface card — a JSON file downloads containing:
- Interface configuration (keys, address, port, AWG2 parameters)
- All peers (keys, allowedIPs, settings)

#### Full System Backup

The **Backup** button in the top navigation downloads a complete configuration dump.

#### Restore

**Restore** → select a JSON backup file. The configuration will be restored over the existing one.

---

## Appendix: Common Scenarios

### Scenario 1: Simple Client VPN

1. **Settings** → set DNS, set Default Client Allowed IPs = `0.0.0.0/0, ::/0`
2. **Settings → AWG2 Templates** → create a template with TLS 1.3 or DNS Query profile
3. **Interfaces → + New Interface** → Protocol: AmneziaWG 2.0, Address: `10.8.0.1/24`, apply template
4. **Start** the interface
5. **+ New Peer** → enter name → share QR code with the client

### Scenario 2: Cascaded VPN (Traffic Through Two Servers)

```
Client → Server A (wg10) → Server B (wg11) → Internet
```

1. **Server B**: create interface wg11 (10.200.0.1/24), create a client peer for Server A
2. **Server A**: create interface wg10 (10.100.0.1/24) for clients; create an S2S interface to connect to Server B
3. **Server A → Firewall → Rules**: Source = 10.100.0.0/24, Gateway = wg11-interface
4. Server A's clients automatically exit through Server B

### Scenario 3: Country-Based Routing

```
Client → Server:
  - Traffic to RU sites → through Russian ISP
  - Everything else → through foreign VPN
```

1. **Gateways**: create "RU ISP" (eth0) and "Abroad VPN" (wg20)
2. **Aliases**: create ipset "ru_prefixes" → Generate → Country: RU
3. **Firewall → Rules**:
   - Rule 1: Dst = `ru_prefixes`, Action = Accept, Gateway = RU ISP
   - Rule 2: Dst = any, Action = Accept, Gateway = Abroad VPN
4. **Route Test**: verify that `8.8.8.8` goes through Abroad VPN and `ya.ru` through RU ISP
