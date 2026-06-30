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
13. [Multi-Server Management 🆕](#13-multi-server-management-)
14. [Speed Test 🆕](#14-speed-test-)
15. [Monitoring & Diagnostics 🆕](#15-monitoring--diagnostics-)
16. [Rate Limits 🆕](#16-rate-limits-)
17. [Wizards 🆕](#17-wizards-)

**Appendices:**
- [Appendix A: Common Scenarios](#appendix-common-scenarios)
  - [Scenario 1: Simple Client VPN](#scenario-1-simple-client-vpn)
  - [Scenario 2: Cascaded VPN](#scenario-2-cascaded-vpn-traffic-through-two-servers)
  - [Scenario 3: Country-Based Routing](#scenario-3-country-based-routing)
  - [Scenario 4: Relay Third-Party WireGuard via DNAT](#scenario-4-relay-third-party-wireguard-via-dnat)
  - [Scenario 5: Cascade as a WireGuard Client (Uplink + PBR)](#scenario-5-cascade-as-a-wireguard-client-uplink--pbr)
- [Appendix B: Transit (Relay) Server](#appendix-b-transit-relay-server)

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
| **MSS Clamping** | Clamp TCP MSS on this interface. Only available for client interfaces (Disable Routes = off). `Disabled` — off; `Auto (PMTU)` — clamp to path MTU automatically; `Manual` — set a fixed value (e.g. 1280 for tunnel-over-tunnel scenarios). Applied in both directions via iptables mangle |

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
| **Group** | Client Group for this peer. Determines which ipset alias the peer's IP is added to — used in firewall rules. Defaults to the `default` group |

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
- **Bandwidth Limit** — per-client speed limit (see below)

### Bandwidth Limiting

Allows you to independently limit the data transfer speed for each client.

| Field | Description |
|-------|-------------|
| **↓ Download** | Maximum download speed for the client (server → client), Mbit/s |
| **↑ Upload** | Maximum upload speed from the client (client → server), Mbit/s |

**Example values:** `10` = 10 Mbit/s, `0.5` = 500 Kbit/s, `0` = unlimited.

Limiting is implemented via Linux `tc` (`HTB` for egress and `police` for ingress) and takes effect immediately — no interface restart required. WireGuard overhead (~5%) is compensated automatically: specifying 10 Mbit/s gives the client exactly 10 Mbit/s on a speed test.

When a limit is set, colored badges appear on the peer card:
- 🔴 `↓10M` — download limit
- 🟢 `↑5M` — upload limit

### Enabling / Disabling a Peer

Use the toggle on the peer card. A disabled peer is excluded from the WireGuard configuration — no traffic passes.

### Statistics

The peer card displays:

- **Online** — blinking red dot = peer transferred data within the last ~3 minutes
- **Last Handshake** — time of the last successful WireGuard handshake. Shows **"Never"** for newly created peers that have not yet connected
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
| **client-group** | Peer group — kernel ipset auto-managed by peer membership | — managed automatically |
| **group** | Combination of host/network aliases | — select from existing aliases |
| **port** | Ports and ranges | `80`, `443`, `8080-8090` |
| **port-group** | Combination of port aliases | — select from existing port aliases |

### Client Groups

A Client Group is a special alias type that automatically tracks the IPs of client peers assigned to it. When a peer is added to a group its IP is immediately inserted into the kernel ipset; when removed it is cleaned up. Use client groups in firewall rules to apply policies to a whole set of clients at once.

- The `default` group is created automatically on first start and cannot be deleted
- Every new client peer is placed in `default` unless you choose another group at creation time
- Create a new group: **+ Add Alias → type: Client Group**
- Reassign a peer's group: **Edit Peer → Group**
- Deleting a group moves all its peers to `default`
- Hovering the group badge in a firewall rule shows the actual IPs from the ipset

### Creating an Alias

**Firewall → Aliases → + Add**

1. Select the type
2. Enter entries (one per line)
3. For group/port-group — select members from the list
4. For client-group — just set a name; contents are managed automatically

### Populating an ipset Alias

Three methods are available for **ipset** type aliases (in priority order when creating):

#### 1. Manual CIDR Entry

In the **Enter CIDRs manually** section, enter prefixes one per line. They are loaded into the kernel ipset on save. Takes priority over file upload.

#### 2. Upload from File

Click **Choose CIDR file** and select a text file with CIDR prefixes (one per line; lines starting with `#` are ignored). When creating — upload happens immediately after the alias is saved.

#### 3. Generate from RIPE NCC (Countries and AS Numbers)

In the **Generate** section, select the source:
- **Country** — enter a country code (RU, US, CN, etc.)
- **ASN** — enter an autonomous system number
- **ASN List** — multiple AS numbers separated by commas

Click **Generate** — prefixes are fetched from RIPE NCC asynchronously.

### Editing an ipset Alias

When clicking **Edit** on an ipset alias, the behavior depends on the set size:

| Condition | What is shown |
|-----------|---------------|
| ≤ 200 entries, not generated | Textarea with current content — edit and click Save |
| > 200 entries or generated | Entry count + **Replace from file** button |

### Interactive Alias Badges

In NAT and Firewall rule tables, aliases are shown as purple badges that support:

- **Hover** — a popup showing the alias entries (for ipset: first 20 + "... and N more")
- **Click** — navigates to Firewall → Aliases with the edit modal open for that alias

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
| **MTU** | MTU written into client configs. `0` = omit (WireGuard picks automatically). Typical values: `1420` (WireGuard default), `1280` (safe for all networks including restrictive ISPs). Can be overridden per-interface via **Edit Interface → MTU Override** |
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

### Expired Peer Policy

Controls what happens to client peers when their expiry date is reached.

| Value | Behaviour |
|-------|-----------|
| **Disable** (default) | Peer is disabled (`enabled = false`) — excluded from the WireGuard config, no traffic passes |
| **Restrict** | Peer remains enabled but is rate-limited and moved to a specified group. Original group and rate-limit settings are restored automatically when the expiry date is extended |

When **Restrict** is selected, two additional fields appear:

| Field | Description |
|-------|-------------|
| **Rate Limit Down** | Download cap applied to the peer at expiry (kbps) |
| **Rate Limit Up** | Upload cap applied to the peer at expiry (kbps) |
| **Move to Group** | Client group the peer is moved to at expiry |

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

### Version Notifications

When a new Cascade release is available, a bell icon (🔔) appears in the header. Clicking it opens a changelog dialog showing what is new in the latest version.

To update the server, run on the host:

```bash
cd /root/cascade
git pull
docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml down
docker compose -f docker-compose.yml up -d
```

### Backup and Restore

#### Interface Backup

Click **Backup** on the interface card — a JSON file downloads containing:
- Interface configuration (keys, address, port, AWG2 parameters)
- All peers (keys, allowedIPs, settings)

#### Interface Restore

**Restore** → select a JSON backup file. The interface configuration and all peers will be restored.

#### Full System Backup (Settings → System Backup)

**Settings → System Backup → Download Backup**

A dialog opens with an optional password field:

| Password | Result |
|----------|--------|
| Not set | `cascade-backup-YYYYMMDD.tar.gz` — plain archive |
| Set | `cascade-backup-YYYYMMDD.tar.gz.enc` — AES-256-GCM encrypted |

Archive contents:
- `cascade.db` — all configuration: interfaces, peers, NAT/Firewall rules, aliases, users, gateways
- `*.save` — kernel ipset alias contents

> ⚠️ **If the password is lost, the backup cannot be decrypted.** AES-256-GCM uses authenticated encryption — brute-forcing the password is not feasible without knowing the original.

> **Not included in the backup:** `.env`, `docker-compose.yml`, TLS certificates — transfer these separately.

#### Full System Restore

**Settings → System Backup → Restore Backup**

Select a `.tar.gz` or `.tar.gz.enc` file.

- **Unencrypted file** — restore begins immediately
- **Encrypted file** — a password field appears before restore starts

**Security:** if the wrong password is entered, AES-256-GCM authentication fails **before** any file is written to disk. Your data is not affected.

After a successful restore, the system:
1. Replaces `awg.db` and `*.save` files with the archive contents
2. Restarts the container (requires `restart: always` in `docker-compose.yml`)
3. Reloads the page after ~4 seconds

> **Warning:** Restore replaces ALL current data. After restart, all active WireGuard sessions will be dropped — clients will need to reconnect.

#### Migrating to a New Server

1. On the old server: **Settings → System Backup → Download Backup**
2. Transfer manually: `.env`, `docker-compose.yml` (and TLS certificates if applicable)
3. On the new server: install Cascade (installation script)
4. Log in with any temporary password (Welcome screen)
5. **Settings → System Backup → Restore Backup** → select the `.tar.gz` file
6. Server restarts (~5 seconds)
7. **Log in with the old server's username and password** — the database was replaced, the temporary account is gone
8. Update DNS / firewall rules to the new IP
9. Notify users of the new endpoint (server IP has changed)

> **What is restored:** all interfaces (start automatically), peers, NAT/Firewall/Aliases/Gateways, ipset alias contents, users, API tokens, TOTP.

#### Migrating from AWG-Easy (Import Backup)

If you previously used **AWG-Easy** and want to move your configuration to Cascade **without reissuing client configs**:

1. Download the AWG-Easy backup file (`wg0.json` or similar)
2. **Interfaces → Import Backup** (purple button)
3. Click **"Browse…"** and select the JSON file
4. Enter the **UDP Listen Port** (must not conflict with existing interfaces)
5. Click **"Import & Start"**

Cascade will create a new interface and recreate all clients:
- **Server and client keys are preserved as-is** — clients do not need to update their configs
- **QR codes are available** immediately — client private keys are stored in the database
- **AWG2 obfuscation parameters** are imported automatically (Jc, H1–H4, S1–S4)
- Disabled clients from AWG-Easy remain disabled

> **Port or address space conflict** — the import is cancelled entirely with an explanation.

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

### Scenario 4: Relay Third-Party WireGuard via DNAT

**Use case:** the client has a config from a third-party WireGuard server (NL, DE, etc.) but
cannot connect to it directly. The Cascade intermediate server transparently forwards traffic —
the client is unaware of the real server.

```
Client ──UDP:51820──► Cascade RU ──UDP:51820──► WG server NL
                      (DNAT + MASQUERADE)        (any, not Cascade)
```

The WireGuard session is **end-to-end** between client and NL server. Cascade RU only forwards
encrypted UDP datagrams and cannot see the traffic content.

**Setup on Cascade RU — one rule:**

1. **NAT → Port Forwarding → + New Rule**
   - Protocol: `UDP`
   - In Port: `51820` (port from NL server config)
   - Redirect to Host: `<NL server IP>`
   - Redirect to Port: `51820`
   - Interface: external interface (`eth0`)
   - **Masquerade: ✅** — required; without it NL server replies directly to the client, bypassing Cascade RU

No WireGuard interface on Cascade RU is needed.

**What the client changes in their config** (received from NL server):

```ini
[Peer]
PublicKey = <NL server public key — unchanged>
Endpoint = <Cascade RU IP>:51820   # ← change only this
AllowedIPs = 0.0.0.0/0             # unchanged
```

> **Note:** if Cascade RU already has its own WireGuard interface on port 51820, use a different
> In Port (e.g. 51821) — the client connects to 51821, Cascade forwards to NL server's 51820.

### Scenario 5: Cascade as a WireGuard Client (Uplink + PBR)

**Use case:** you have a ready-made `.conf` file from a third-party WireGuard server
(VPN provider, colleague's server, any WireGuard server not running Cascade). You want
to connect Cascade to that server and use the tunnel as a gateway for selective
policy-based routing — routing specific traffic through the foreign server.

```
Clients → Cascade (wg11: uplink to NL server) → NL WG server → Internet
                   ↑
            PBR: selected traffic only
```

**Key properties:**
- The kernel routing table is **not modified** — existing clients and rules remain intact
- The address from `.conf` is converted to `/32` — subnet conflicts are prevented
- Traffic control is via Firewall → Rules (PBR)

**Step 1 — Import the `.conf` file:**

1. **Interfaces → Import .conf**
2. Click **"Browse file…"** and select the `.conf`, or paste the content manually
3. The **Name** field is pre-filled from the filename — edit if needed
4. Click **"Import & Start"**

Cascade creates an interface (e.g. `wg11`) with the address from `.conf` (e.g. `10.8.0.5/32`)
and automatically adds an upstream peer with the remote server's public key and endpoint.

> The interface will connect and WireGuard handshake with the remote server happens
> automatically — no additional setup on the server side is needed (it already knows your key).

**Step 2 — Outbound NAT on wg11 (required):**

Client traffic leaves Cascade towards the NL server with the client's inner source IP
(e.g. `10.8.0.2`). The NL server has no route to the `10.8.0.0/24` subnet and cannot
reply — the traffic is one-way. The fix: MASQUERADE rewrites the source IP to Cascade's
own tunnel address (`10.8.0.5`), which the NL server knows as a registered peer.

1. **NAT → Outbound → + New Rule**
   - Name: `Masquerade via NL Uplink`
   - Source: client subnet (e.g. `10.8.0.0/24`, or `any` for all interfaces)
   - Outbound Interface: `wg11`
   - Type: `MASQUERADE`

**Step 3 — Create a gateway for PBR:**

1. **Gateways → + New Gateway**
   - Name: `NL Uplink`
   - Interface: `wg11`
   - Gateway IP: remote server's tunnel IP (e.g. `10.8.0.1` — the first address in the subnet)
   - Monitor Address: leave empty (= ping Gateway IP)

Cascade automatically adds a `/32` host route for the gateway IP via wg11 so that monitoring
can ping the inner tunnel IP.

**Step 4 — Configure PBR rules:**

1. **Firewall → Rules → + New Rule**
   - Source: your client subnet (or `any`)
   - Destination: target resources (alias, subnet, or `any`)
   - Action: `Accept`
   - Gateway: `NL Uplink`

Traffic matching the rule is routed through wg11 to the NL server.
All other traffic uses the normal routing path.

**Verification:**

```bash
# From a client connected to Cascade:
curl ifconfig.io        # should return the NL server's IP for traffic matching the rule
```

The wg11 interface card in Cascade will show RX/TX counters and latest handshake time.

---

## Appendix B: Transit (Relay) Server

**Use case:** the main VPN server (Server B) is inaccessible to clients directly — blocked,
in a private network, or its real IP needs to be hidden. The relay server (Server A) accepts
client UDP traffic and transparently forwards it to Server B.

```
Client ──UDP:51820──► Server A ──UDP:51820──► Server B ──► Internet
            public IP             hidden server
            (visible to clients)  (real VPN)
```

The WireGuard session is **end-to-end** between client and Server B — Server A only forwards
UDP datagrams and cannot see the traffic content.

**Why two NAT rules on Server A:**

1. **DNAT** — changes the destination: `Client→A:51820` becomes `Client→B:51820`
2. **MASQUERADE** — changes the source: `Client→B:51820` becomes `A→B:51820`
   Without MASQUERADE, Server B would reply **directly to the client** (which it cannot reach),
   and the tunnel would not work.

**Setup on Server B** (normal WireGuard/AWG server):

1. **Interfaces → + New Interface**
   - Address: `10.8.0.1/24`
   - Listen Port: `51820`
   - **Host (Endpoint)**: `<Server A public IP>` ← this goes into client peer configs
2. Start the interface — Cascade automatically adds MASQUERADE for client internet traffic
3. Create peers — downloaded configs will have endpoint `<Server A IP>:51820`

**Setup on Server A** (pure relay, no WireGuard interface):

**Step 1 — Port Forwarding (DNAT):**

1. **NAT → Port Forwarding → + New Rule**
   - Protocol: `UDP`
   - In Port: `51820`
   - Redirect to Host: `<Server B IP>`
   - Redirect to Port: `51820`
   - Interface: external interface (`eth0`)

**Step 2 — Outbound NAT (MASQUERADE):**

1. **NAT → Outbound → + New Rule**
   - Name: `Masquerade to Server B`
   - Source: `any`
   - Outbound Interface: interface towards Server B (usually `eth0`)
   - Type: `MASQUERADE`

**Verification:**

```bash
# On client: connect to <Server A IP>:51820
ping 10.8.0.1          # ping to Server B gateway should work
curl ifconfig.io       # should show Server B's exit IP (not Server A or client)
```

On Server B, the peer card should show RX/TX and last handshake time — confirming the
tunnel is working through the relay.

---

## 13. Multi-Server Management 🆕

Cascade lets you manage multiple routers from a single browser session. All communication
with remote servers is proxied through the local server — the browser never contacts remote
servers directly, and tokens are never exposed to the client.

### Adding a Remote Server

Click **"+ Add Server"** in the **Remotes** sidebar section.

**Login mode** (recommended):

| Field | Description |
|-------|-------------|
| **Name** | Display name for this server in the sidebar |
| **URL** | Base URL of the remote Cascade instance (e.g. `https://1.2.3.4/secret-path`) |
| **Username / Password** | Credentials on the remote server |
| **TOTP code** | 6-digit code — appears automatically if the remote has 2FA enabled |
| **Skip TLS verification** | Enable for servers with self-signed certificates |

**Token mode**: if you have a pre-created API token (`ws_...`) from the remote server,
enter it directly in the **API Token** field instead of username/password.

After saving, Cascade logs into the remote, creates a dedicated API token, and stores it
locally. Your password is never saved.

### Switching Servers

Click any server name in the sidebar. The UI reloads and all subsequent actions target
that server. A badge in the header shows which server is currently active.

Click **Local** to return to the local server at any time.

### Testing and Removing

- **Test** — sends a ping to the remote's `/api/health` endpoint to verify the token is still valid
- **Remove** — deletes the remote entry from the local database (does not affect the remote server)

### Notes

- If the remote becomes unreachable (401 or 5xx), Cascade automatically switches back to local
- The Speed Test orchestration always runs on the local server regardless of which server is active

---

## 14. Speed Test 🆕

Measure throughput between any two managed Cascade servers using iperf3.

### Requirements

- `iperf3` must be installed on **both** the source and destination servers
- The servers must be reachable from each other on the chosen route (internet or tunnel)

```bash
# Install iperf3 on each server
apt install iperf3
```

### Running a Speed Test

Open **Speed Test** from the sidebar (or the Administration page).

| Field | Description |
|-------|-------------|
| **From** | Source server — iperf3 server runs here |
| **To** | Destination server — iperf3 client runs here |
| **Route** | `Auto`, `Tunnel`, `Internet`, or `Manual` — see below |
| **Duration** | Test duration in seconds (default: 10) |
| **Streams** | Parallel iperf3 streams (default: 4) |

### Route Modes

| Mode | Behaviour |
|------|-----------|
| **Auto** | Detects a shared S2S subnet automatically; falls back to internet if none found |
| **Tunnel** | Forces traffic through the WireGuard S2S tunnel between the two servers |
| **Internet** | Uses public IPs regardless of any tunnel |
| **Manual** | Lets you pick specific WireGuard interfaces on each server for the bind address |

### Reading Results

After the test completes the result card shows:

| Metric | Description |
|--------|-------------|
| **Send** | Throughput from source to destination (Mbps) |
| **Receive** | Throughput from destination to source (Mbps) |
| **Retransmits** | TCP retransmissions — high values indicate packet loss |
| **Latency** | Mean RTT across all streams (ms) |

Previous results are saved and visible in the **History** table below the test form.

---

## 15. Monitoring & Diagnostics 🆕

### Dashboard Layout

The Dashboard supports **free widget placement** — drag any widget card to any position on the grid. Widgets snap to the grid but are not forced into a compact column layout, so you can leave empty space between them.

- **Resize** — drag the bottom-right corner of any widget
- **Move** — drag the widget header to reposition it
- **Zoom** — use the **+** / **−** buttons on widget cards (except monitoring charts) to scale the content. Zoom is saved per widget across page reloads
- **Add Widget** — click **"+ Add Widget"** to insert a new widget from the available types

### Traffic Metrics Widget

The **Monitoring** widget on the Dashboard shows real-time TX/RX traffic per WireGuard interface.

- **Period selector** — choose between 5 min (live), 1 h, 24 h, or 7 days
- Each interface gets its own area chart; colors are consistent across reloads
- Charts pause automatically when the browser tab is hidden to save resources
- The widget can be added to any dashboard page via **Add Widget**

### Diagnostics Page

The **Diagnostics** page (sidebar) shows all monitoring widgets in a full-screen layout:

- Per-interface traffic charts for all active interfaces
- Gateway status history charts for all gateway groups

Use this page for a quick overview of the router's health without navigating between sections.

### Gateway Status History

Each gateway group card on the Diagnostics page includes a **stacked bar chart** showing
the distribution of gateway states over time:

| Color | State |
|-------|-------|
| Green | Online |
| Yellow | Degraded |
| Red | Offline |
| Gray | Admin down (disabled) |

Hover over any bar to see the exact percentage for each state in that time bucket.

---

## 16. Rate Limits 🆕

Bandwidth limits can be applied per **client group** to cap the download and upload speed
of all peers in that group. Limits are enforced per individual IP using Linux Traffic Control (tc HTB).

### Configuring Rate Limits

1. Go to **Firewall → Aliases**
2. Open an existing **Client Group** alias or create a new one
3. Set **Rate Limit Down** (kbps) and **Rate Limit Up** (kbps)
4. Save — limits are applied immediately to all active peers in the group

| Field | Description |
|-------|-------------|
| **Rate Limit Down** | Maximum download speed per client IP in kbps. `0` = unlimited |
| **Rate Limit Up** | Maximum upload speed per client IP in kbps. `0` = unlimited |

### Notes

- Limits apply per IP address inside the group, not shared across the group
- When a peer moves to a different group, the old limit is removed and the new group's limit is applied
- Limits are restored automatically when a WireGuard interface is started or restarted
- Setting both fields to `0` removes all tc rules for the group

---

## 17. Wizards 🆕

Wizards guide you through multi-step configuration scenarios automatically, creating all required objects (interfaces, aliases, gateways, firewall rules, NAT) in the correct order.

Access wizards via the **Wizards** section in the sidebar (click to expand/collapse).

---

### Wizard: Simple Client VPN

Creates a ready-to-use WireGuard/AWG interface for client peers in a few clicks.

**Steps:**
1. Choose protocol (WireGuard or AmneziaWG)
2. Name the interface and set DNS
3. Add the first peer
4. Done — QR code and config are ready to share

---

### Wizard: Cascade via WireGuard Uplink

Connects Cascade as a client to an upstream WireGuard server and routes selected client traffic through it using PBR.

**Use case:** route specific clients or destinations through a rented VPN server, while keeping other traffic on the default gateway.

**Steps:**
1. **Import `.conf`** — paste or upload the upstream server's WireGuard config. The wizard parses the public key, endpoint, and allowed IPs automatically.
2. **Source** — select which client interfaces (and their peers) should use this uplink. The wizard can create a source alias automatically.
3. **Destination** — choose what traffic to route through the uplink: all traffic, specific countries (GeoIP), or an AS number.
4. **Options** — set interface name, gateway name, MSS clamping, and fallback policy.
5. **Apply** — the wizard creates the uplink interface, starts it, creates aliases, a gateway, a PBR firewall rule, and a masquerade NAT rule.

> **Note:** The uplink interface is created with **Disable Routes** enabled — routing is handled entirely by the PBR rule, not by wg-quick.

---

### Wizard: Cascade ↔ Cascade S2S

Interconnects two Cascade routers over a WireGuard or AWG site-to-site tunnel and sets up PBR so selected clients on the local server route through the remote server.

**Prerequisites:** both servers must be added to **Multi-Server Management** (Settings → Remotes).

**Steps:**
1. **Remote** — select the remote Cascade server. If not yet added, fill in the inline form (URL + password or API token).
2. **Source** — select local client interfaces whose traffic should be routed through the S2S tunnel.
3. **Destination** — choose what traffic to forward: all, specific countries, or an AS number.
4. **Options** — set interface names, protocol (WireGuard / AWG), MSS clamping, and fallback policy.
5. **Apply** — the wizard:
   - Allocates a `/30` subnet from `10.255.255.0/24` for the S2S link
   - Creates a local S2S interface and a remote S2S interface (via API)
   - Exchanges public keys and PSK between both sides (correct PSK sync order)
   - Creates source alias, destination alias, gateway, PBR firewall rule, and NAT on the local server
   - Creates a return route and NAT on the remote server

> **PSK sync:** the wizard first imports local params into the remote (which generates the PSK), then re-exports remote params (PSK now included) and imports into local — both sides end up with the same PSK.
