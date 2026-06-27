// Package db manages the SQLite database lifecycle.
//
// Two files:
//   <dataDir>/cascade.db — config, users, peers, rules (included in backups)
//   <dataDir>/metrics.db — metrics_history only (large, exclude from backups)
//
// Design decisions:
//   - modernc.org/sqlite: pure Go, no CGO → static binary (CGO_ENABLED=0)
//   - WAL journal mode: concurrent reads + serialised writes
//   - MaxOpenConns=1: prevents "database is locked" on concurrent writes
//   - Version-based migrations: schema evolves safely across upgrades
package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

var instance *sql.DB
var metricsInstance *sql.DB

// migrateDBName renames wireguard.db → cascade.db (and WAL/SHM siblings) when
// upgrading from the old naming scheme. Runs unattended: logs what it does,
// never fails the startup — if rename fails the old file is used as-is.
func migrateDBName(dataDir string) {
	oldBase := filepath.Join(dataDir, "wireguard.db")
	newBase := filepath.Join(dataDir, "cascade.db")

	// Nothing to do if the new name already exists or the old one is absent.
	if _, err := os.Stat(newBase); err == nil {
		return // cascade.db already present — skip
	}
	if _, err := os.Stat(oldBase); os.IsNotExist(err) {
		return // neither file exists — fresh install
	}

	// Rename main DB file.
	if err := os.Rename(oldBase, newBase); err != nil {
		log.Printf("db: rename wireguard.db → cascade.db failed: %v (continuing with old name)", err)
		return
	}
	log.Printf("db: renamed wireguard.db → cascade.db")

	// Rename WAL / SHM siblings — ignore errors (they may not exist).
	for _, ext := range []string{"-wal", "-shm"} {
		_ = os.Rename(oldBase+ext, newBase+ext)
	}
}

// Init opens (or creates) cascade.db in dataDir and runs all pending migrations.
// Must be called once at startup before any other package uses DB().
func Init(dataDir string) error {
	migrateDBName(dataDir)

	path := filepath.Join(dataDir, "cascade.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("db open %s: %w", path, err)
	}

	// Single connection — SQLite supports one writer at a time.
	// WAL mode allows concurrent readers alongside the writer.
	db.SetMaxOpenConns(1)

	// Performance and safety pragmas.
	pragmas := []string{
		`PRAGMA journal_mode=WAL`,      // concurrent reads, faster writes
		`PRAGMA foreign_keys=ON`,       // enforce FK constraints
		`PRAGMA busy_timeout=5000`,     // wait up to 5s on lock instead of SQLITE_BUSY
		`PRAGMA synchronous=NORMAL`,    // safe with WAL, faster than FULL
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	instance = db

	if err := runMigrations(db); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	log.Printf("db: opened %s", path)

	// Open separate metrics DB — large, not included in config backups.
	if err := initMetricsDB(dataDir); err != nil {
		return fmt.Errorf("metrics db: %w", err)
	}

	return nil
}

// DB returns the main config database handle.
// Panics if Init() has not been called.
func DB() *sql.DB {
	if instance == nil {
		panic("db.Init() must be called before db.DB()")
	}
	return instance
}

// MetricsDB returns the metrics-only database handle (metrics.db).
// Panics if Init() has not been called.
func MetricsDB() *sql.DB {
	if metricsInstance == nil {
		panic("db.Init() must be called before db.MetricsDB()")
	}
	return metricsInstance
}

// Close closes both databases. Call on graceful shutdown.
func Close() {
	if metricsInstance != nil {
		metricsInstance.Close()
		metricsInstance = nil
	}
	if instance != nil {
		instance.Close()
		instance = nil
	}
}

// initMetricsDB opens (or creates) metrics.db and ensures the schema exists.
func initMetricsDB(dataDir string) error {
	path := filepath.Join(dataDir, "metrics.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}

	db.SetMaxOpenConns(1)

	pragmas := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA synchronous=NORMAL`,
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	schema := []string{
		`CREATE TABLE IF NOT EXISTS metrics_history (
			ts  INTEGER NOT NULL,
			key TEXT    NOT NULL,
			val REAL    NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS metrics_history_key_ts ON metrics_history(key, ts)`,
	}
	for _, s := range schema {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("schema: %w", err)
		}
	}

	metricsInstance = db
	log.Printf("db: opened %s", path)
	return nil
}

// ── Migrations ────────────────────────────────────────────────────────────────

type migration struct {
	version int
	sql     string
}

// migrations is the ordered list of all schema changes.
// NEVER modify an existing migration — always add a new one.
var migrations = []migration{
	{
		version: 1,
		sql: `
-- ── Global settings (key/value) ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

-- ── AWG2 obfuscation templates ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS templates (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    is_default INTEGER NOT NULL DEFAULT 0,  -- boolean: 0/1
    jc         INTEGER NOT NULL DEFAULT 6,
    jmin       INTEGER NOT NULL DEFAULT 10,
    jmax       INTEGER NOT NULL DEFAULT 50,
    s1         INTEGER NOT NULL DEFAULT 64,
    s2         INTEGER NOT NULL DEFAULT 67,
    s3         INTEGER NOT NULL DEFAULT 64,
    s4         INTEGER NOT NULL DEFAULT 4,
    h1         TEXT    NOT NULL DEFAULT '',  -- "start-end" range string (FIX-4)
    h2         TEXT    NOT NULL DEFAULT '',
    h3         TEXT    NOT NULL DEFAULT '',
    h4         TEXT    NOT NULL DEFAULT '',
    i1         TEXT    NOT NULL DEFAULT '',  -- protocol imitation packet
    i2         TEXT    NOT NULL DEFAULT '',
    i3         TEXT    NOT NULL DEFAULT '',
    i4         TEXT    NOT NULL DEFAULT '',
    i5         TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- ── Tunnel interfaces (wg10, wg11, …) ───────────────────────────────────────
CREATE TABLE IF NOT EXISTS interfaces (
    id           TEXT PRIMARY KEY,            -- e.g. "wg10"
    name         TEXT NOT NULL DEFAULT '',
    address      TEXT NOT NULL DEFAULT '',    -- CIDR e.g. "10.8.0.1/24"
    listen_port  INTEGER NOT NULL DEFAULT 555,
    protocol     TEXT NOT NULL DEFAULT 'wireguard-1.0',  -- or "amneziawg-2.0"
    enabled      INTEGER NOT NULL DEFAULT 0,
    disable_routes INTEGER NOT NULL DEFAULT 0,
    private_key  TEXT NOT NULL DEFAULT '',
    public_key   TEXT NOT NULL DEFAULT '',
    -- AWG2 obfuscation params (NULL for WireGuard 1.0 interfaces)
    jc   INTEGER, jmin INTEGER, jmax INTEGER,
    s1   INTEGER, s2   INTEGER, s3   INTEGER, s4 INTEGER,
    h1   TEXT,    h2   TEXT,    h3   TEXT,    h4 TEXT,
    i1   TEXT,    i2   TEXT,    i3   TEXT,    i4 TEXT, i5 TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ── Peers ────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS peers (
    id                    TEXT PRIMARY KEY,
    interface_id          TEXT NOT NULL REFERENCES interfaces(id) ON DELETE CASCADE,
    name                  TEXT NOT NULL DEFAULT '',
    public_key            TEXT NOT NULL DEFAULT '',
    private_key           TEXT NOT NULL DEFAULT '',  -- empty for interconnect peers
    preshared_key         TEXT NOT NULL DEFAULT '',
    allowed_ips           TEXT NOT NULL DEFAULT '',  -- e.g. "10.8.0.2/32"
    client_allowed_ips    TEXT NOT NULL DEFAULT '0.0.0.0/0, ::/0',
    persistent_keepalive  INTEGER NOT NULL DEFAULT 25,
    peer_type             TEXT NOT NULL DEFAULT 'client',  -- "client" or "interconnect"
    enabled               INTEGER NOT NULL DEFAULT 1,
    created_at            TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_peers_interface ON peers(interface_id);

-- ── Static routes ────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS routes (
    id          TEXT PRIMARY KEY,
    destination TEXT NOT NULL DEFAULT '',   -- CIDR e.g. "192.168.1.0/24"
    via         TEXT NOT NULL DEFAULT '',   -- next-hop IP (empty if dev-only)
    dev         TEXT NOT NULL DEFAULT '',   -- interface name (empty if via-only)
    metric      INTEGER NOT NULL DEFAULT 0,
    table_name  TEXT NOT NULL DEFAULT 'main',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ── NAT rules ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS nat_rules (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL DEFAULT '',
    source          TEXT NOT NULL DEFAULT '',   -- CIDR or empty
    source_alias_id TEXT NOT NULL DEFAULT '',   -- alias id (empty if direct CIDR)
    out_interface   TEXT NOT NULL DEFAULT '',
    type            TEXT NOT NULL DEFAULT 'MASQUERADE',  -- or "SNAT"
    to_source       TEXT NOT NULL DEFAULT '',   -- for SNAT
    comment         TEXT NOT NULL DEFAULT '',
    enabled         INTEGER NOT NULL DEFAULT 1,
    order_idx       INTEGER NOT NULL DEFAULT 0,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ── Firewall aliases ─────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS aliases (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE COLLATE NOCASE,
    type           TEXT NOT NULL DEFAULT 'host',  -- host/network/ipset/group/port/port-group
    entries        TEXT NOT NULL DEFAULT '[]',    -- JSON array
    generator_opts TEXT NOT NULL DEFAULT '{}',    -- JSON: {source, country, asn, ...}
    created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ── Firewall rules ───────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS firewall_rules (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL DEFAULT '',
    interface           TEXT NOT NULL DEFAULT '',
    protocol            TEXT NOT NULL DEFAULT 'any',
    source              TEXT NOT NULL DEFAULT '{}',  -- JSON: {type, value, aliasId, not}
    destination         TEXT NOT NULL DEFAULT '{}',  -- JSON: {type, value, aliasId, not}
    src_port            TEXT NOT NULL DEFAULT '',    -- alias id for port matching
    dst_port            TEXT NOT NULL DEFAULT '',
    action              TEXT NOT NULL DEFAULT 'ACCEPT',  -- ACCEPT/DROP/REJECT
    gateway_id          TEXT NOT NULL DEFAULT '',    -- empty = no PBR
    fallback_to_default INTEGER NOT NULL DEFAULT 0,
    enabled             INTEGER NOT NULL DEFAULT 1,
    order_idx           INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ── Gateways ─────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS gateways (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL DEFAULT '',
    interface        TEXT NOT NULL DEFAULT '',
    gateway_ip       TEXT NOT NULL DEFAULT '',
    monitor_address  TEXT NOT NULL DEFAULT '',
    monitor_interval INTEGER NOT NULL DEFAULT 10,   -- seconds
    window_seconds   INTEGER NOT NULL DEFAULT 30,
    monitor_http     TEXT NOT NULL DEFAULT '{}',    -- JSON: {enabled, url, interval, ...}
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ── Gateway groups ───────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS gateway_groups (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    trigger    TEXT NOT NULL DEFAULT 'packetloss',  -- packetloss/latency/packetloss_latency
    members    TEXT NOT NULL DEFAULT '[]',           -- JSON: [{gatewayId, tier}]
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ── Migration version tracker ────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`,
	},
	{
		version: 2,
		sql: `
-- Add missing columns to aliases table (present in AliasManager.js model).
-- SQLite does not support adding multiple columns in one ALTER TABLE statement.
ALTER TABLE aliases ADD COLUMN description  TEXT    NOT NULL DEFAULT '';
ALTER TABLE aliases ADD COLUMN member_ids   TEXT    NOT NULL DEFAULT '[]';
ALTER TABLE aliases ADD COLUMN ipset_name   TEXT    NOT NULL DEFAULT '';
ALTER TABLE aliases ADD COLUMN entry_count  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE aliases ADD COLUMN last_updated TEXT    NOT NULL DEFAULT '';
`,
	},
	{
		version: 3,
		sql: `
-- Rebuild routes table:
--   1. Add description column (was missing from v1)
--   2. Make metric nullable (NULL = no explicit metric, was NOT NULL DEFAULT 0)
CREATE TABLE routes_new (
    id          TEXT    PRIMARY KEY,
    description TEXT    NOT NULL DEFAULT '',
    destination TEXT    NOT NULL DEFAULT '',
    via         TEXT    NOT NULL DEFAULT '',
    dev         TEXT    NOT NULL DEFAULT '',
    metric      INTEGER,
    table_name  TEXT    NOT NULL DEFAULT 'main',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO routes_new (id, destination, via, dev, metric, table_name, enabled, created_at)
    SELECT id, destination, via, dev, NULLIF(metric, 0), table_name, enabled, created_at
    FROM routes;
DROP TABLE routes;
ALTER TABLE routes_new RENAME TO routes;
`,
	},
	{
		version: 4,
		sql: `
-- Add missing columns to gateways table (present in Gateway.js model but absent from v1 schema).
ALTER TABLE gateways ADD COLUMN monitor           INTEGER NOT NULL DEFAULT 1;
ALTER TABLE gateways ADD COLUMN latency_threshold INTEGER NOT NULL DEFAULT 500;
ALTER TABLE gateways ADD COLUMN monitor_rule      TEXT    NOT NULL DEFAULT 'icmp_only';
ALTER TABLE gateways ADD COLUMN description       TEXT    NOT NULL DEFAULT '';

-- Add description to gateway_groups.
ALTER TABLE gateway_groups ADD COLUMN description TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 5,
		sql: `
-- Add missing columns to firewall_rules table.
-- JS model has fwmark, gatewayGroupId, log, comment which were absent from v1 schema.
ALTER TABLE firewall_rules ADD COLUMN fwmark           INTEGER;           -- nullable, auto-assigned per PBR rule
ALTER TABLE firewall_rules ADD COLUMN gateway_group_id TEXT    NOT NULL DEFAULT '';
ALTER TABLE firewall_rules ADD COLUMN log              INTEGER NOT NULL DEFAULT 0;
ALTER TABLE firewall_rules ADD COLUMN comment          TEXT    NOT NULL DEFAULT '';
`,
	},
	{
		version: 6,
		sql: `
-- Add missing columns to peers table (present in Peer.js model but absent from v1 schema).
ALTER TABLE peers ADD COLUMN endpoint       TEXT NOT NULL DEFAULT '';
ALTER TABLE peers ADD COLUMN address        TEXT NOT NULL DEFAULT '';  -- tunnel IP with iface mask
ALTER TABLE peers ADD COLUMN updated_at     TEXT NOT NULL DEFAULT '';
ALTER TABLE peers ADD COLUMN expired_at     TEXT NOT NULL DEFAULT '';  -- '' = no expiry
ALTER TABLE peers ADD COLUMN one_time_link  TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 7,
		sql: `
-- Multi-user authentication table.
-- Replaces the single PASSWORD_HASH env-var approach.
-- Seeded at startup: if empty and PASSWORD_HASH env is set, admin user is created.
CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL DEFAULT '',
    totp_secret   TEXT NOT NULL DEFAULT '',
    totp_enabled  INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
`,
	},
	{
		version: 8,
		sql: `
-- API tokens for programmatic access.
-- Only the SHA-256 hash of the token is stored — the raw value is shown once at creation.
-- ON DELETE CASCADE: deleting a user revokes all their tokens automatically.
CREATE TABLE IF NOT EXISTS api_tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL DEFAULT '',
    token_hash  TEXT NOT NULL UNIQUE,   -- SHA-256(raw_token) as hex
    last_used   TEXT,                   -- NULL until first use; updated on every authenticated request
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);
`,
	},
	{
		version: 9,
		sql: `
-- Admin role: is_admin flag on users.
-- Admins can manage all users; regular users can only manage themselves.
ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;

-- Grant admin to the user named 'admin' (default installation).
UPDATE users SET is_admin = 1 WHERE username = 'admin';

-- Fallback for custom usernames: if nobody became admin yet,
-- grant admin to the first registered user (oldest created_at).
UPDATE users SET is_admin = 1
WHERE id = (SELECT id FROM users ORDER BY created_at ASC LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM users WHERE is_admin = 1);
`,
	},
	{
		version: 10,
		sql: `
-- Fix: v9 fallback only ran when users table already had rows.
-- On clean installs the table was empty during v9 → first user
-- was created later via UI with is_admin=0.
-- Re-apply the same fallback: grant admin to the oldest user
-- if no admin exists yet.
UPDATE users SET is_admin = 1
WHERE id = (SELECT id FROM users ORDER BY created_at ASC LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM users WHERE is_admin = 1);
`,
	},
	{
		version: 11,
		sql: `
-- Traffic accumulation across container restarts.
-- total_rx / total_tx: lifetime accumulated bytes per peer, flushed to DB
--   every 60 s by the polling goroutine and before every wg-quick down.
-- Initialised to 0; never decremented.  Delta is computed in-memory:
--   delta = max(0, kernelCounter - lastSeen)  — negative means counter reset.
ALTER TABLE peers ADD COLUMN total_rx INTEGER NOT NULL DEFAULT 0;
ALTER TABLE peers ADD COLUMN total_tx INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		version: 12,
		sql: `
-- Per-interface auto-NAT opt-out flag.
-- When nat_disabled=1, generateWgConfig() omits the MASQUERADE PostUp/PostDown line.
-- DEFAULT 0: all existing interfaces keep their current auto-NAT behaviour.
ALTER TABLE interfaces ADD COLUMN nat_disabled INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		version: 13,
		sql: `
-- Port Forwarding (DNAT) rules.
-- Each rule creates three iptables-nft rules:
--   PREROUTING DNAT: redirect inbound traffic on in_port to dest_ip:effective_port
--   FORWARD ACCEPT (new): allow forwarded packets to dest_ip:effective_port
--   FORWARD ACCEPT (return): allow established/related return packets from dest_ip
-- dest_port=0 means "same as in_port" (sentinel; never passed to iptables directly).
CREATE TABLE IF NOT EXISTS nat_dnat_rules (
    id         TEXT PRIMARY KEY,
    name       TEXT    NOT NULL DEFAULT '',
    protocol   TEXT    NOT NULL DEFAULT 'udp',  -- 'tcp' | 'udp' | 'both'
    in_port    INTEGER NOT NULL DEFAULT 0,
    dest_ip    TEXT    NOT NULL DEFAULT '',
    dest_port  INTEGER NOT NULL DEFAULT 0,      -- 0 = same as in_port
    comment    TEXT    NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);
`,
	},
	{
		version: 14,
		sql: `
-- Add optional inbound interface scoping to DNAT rules.
-- Empty string = match any interface (no -i flag in iptables PREROUTING).
-- Typical values: "eth0", "ens3" (WAN interface).
ALTER TABLE nat_dnat_rules ADD COLUMN in_interface TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 15,
		sql: `
-- Add source NAT (masquerade) flag to DNAT rules.
-- When masquerade=1, a POSTROUTING MASQUERADE rule is added scoped to dest_ip:effective_port.
-- Required when the destination host cannot route replies back through this server
-- (i.e. destination is a public server on the internet — the typical port forwarding case).
-- DEFAULT 1: new rules masquerade by default; disable only when destination routes
-- replies back through this server anyway (e.g. hub-and-spoke WireGuard topology).
ALTER TABLE nat_dnat_rules ADD COLUMN masquerade INTEGER NOT NULL DEFAULT 1;
`,
	},
	{
		version: 16,
		sql: `
-- Per-interface public host override.
-- When set, this value is used as the Endpoint in peer client configs instead of
-- the global WG_HOST env / Settings UI → Public IP. Useful for transit (relay) setups
-- where one interface is reachable via a different IP than the server's own public IP.
-- Empty string = use global setting (default behaviour).
ALTER TABLE interfaces ADD COLUMN public_host TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 17,
		sql: `
-- Per-interface MTU override for client configs.
-- 0 = use global MTU from settings (or omit MTU line if global is also 0).
ALTER TABLE interfaces ADD COLUMN mtu INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		version: 18,
		sql: `
-- Per-client bandwidth limits (kbps). 0 = unlimited.
-- Applied via Linux tc HTB (egress) + tc police (ingress) on the WireGuard interface.
ALTER TABLE peers ADD COLUMN rate_down INTEGER NOT NULL DEFAULT 0;
ALTER TABLE peers ADD COLUMN rate_up   INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		version: 19,
		sql: `
-- Uplink flag: set when interface is created via Import .conf (remote server config).
-- Uplink interfaces connect OUT to a remote server; they are not S2S hubs.
-- Export My Params and Import JSON peer buttons are hidden for uplink interfaces.
ALTER TABLE interfaces ADD COLUMN uplink INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		version: 20,
		sql: `
-- Gateway-aware static routes: reference a Gateway or GatewayGroup as next-hop.
-- When set, the route via/dev are resolved dynamically from the gateway at runtime.
-- On gateway status changes the route is updated automatically (failover support).
-- Only one of gateway_id / gateway_group_id may be set (enforced at app layer).
ALTER TABLE routes ADD COLUMN gateway_id       TEXT NOT NULL DEFAULT '';
ALTER TABLE routes ADD COLUMN gateway_group_id TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 21,
		sql: `
-- MSS clamping for client tunnel interfaces (disableRoutes=false).
-- -1 = auto (--clamp-mss-to-pmtu), 0 = disabled, >0 = manual --set-mss value.
-- Applied in PostUp/PostDown iptables-nft TCPMSS rules on both -i and -o directions.
ALTER TABLE interfaces ADD COLUMN mss INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		version: 22,
		sql: `
-- DNAT rules: store user-entered dest (IP or FQDN) separately from the resolved IP.
-- dest           = what the user typed ("1.2.3.4" or "server.example.com")
-- dest_ip        = last successfully resolved IP used in iptables (unchanged for plain IPs)
-- dest_resolved_at = RFC3339 timestamp of last DNS resolution; empty for plain IP rules
-- Existing rules: dest = dest_ip (they were always plain IPs before this migration).
ALTER TABLE nat_dnat_rules ADD COLUMN dest             TEXT NOT NULL DEFAULT '';
ALTER TABLE nat_dnat_rules ADD COLUMN dest_resolved_at TEXT NOT NULL DEFAULT '';
UPDATE nat_dnat_rules SET dest = dest_ip WHERE dest = '';
`,
	},
	{
		version: 23,
		sql: `
-- Client groups: each client peer belongs to a client-group alias (type='client-group').
-- group_id references aliases.id. Empty string = peer not yet assigned (migrated to default on startup).
ALTER TABLE peers ADD COLUMN group_id TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 24,
		sql: `
-- Admin Down: administratively disable a gateway without deleting it.
-- Monitoring continues but effective status is reported as "admin_down".
-- Routing/firewall treat it as down (failover triggers).
ALTER TABLE gateways ADD COLUMN admin_down INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		version: 25,
		sql: `
-- Dashboard: per-user widget layout.
-- widgets stores a JSON array of {id, type, x, y, w, h} objects.
CREATE TABLE IF NOT EXISTS dashboard_widgets (
    user_id TEXT PRIMARY KEY,
    widgets TEXT NOT NULL DEFAULT '[]'
);
`,
	},
	{
		version: 26,
		sql: `
-- Expired peer policy support: remember which group a peer was in before the
-- expiry policy moved it to the configured expired-peer group. Cleared when
-- the peer's expiry date is extended past now (i.e. the peer is "re-activated").
ALTER TABLE peers ADD COLUMN previous_group_id TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 27,
		sql: `
-- Firewall rule separators: visual dividers between rule groups.
-- rule_type = 'rule' (default) | 'separator'.
-- Separator rows have only id, name, order_idx, created_at populated;
-- all other columns are ignored by RebuildChains.
ALTER TABLE firewall_rules ADD COLUMN rule_type TEXT NOT NULL DEFAULT 'rule';
`,
	},
	{
		version: 28,
		sql: `
-- Staged firewall apply: firewall_rules_applied is a snapshot of firewall_rules
-- at the time the user last clicked "Apply". The kernel is always rebuilt from
-- this snapshot, not from the live firewall_rules table.
-- On first start (empty applied table) Init() copies firewall_rules → applied.
CREATE TABLE IF NOT EXISTS firewall_rules_applied (
    id                  TEXT PRIMARY KEY,
    rule_type           TEXT NOT NULL DEFAULT 'rule',
    name                TEXT NOT NULL DEFAULT '',
    interface           TEXT NOT NULL DEFAULT '',
    protocol            TEXT NOT NULL DEFAULT 'any',
    source              TEXT NOT NULL DEFAULT '{}',
    destination         TEXT NOT NULL DEFAULT '{}',
    src_port            TEXT NOT NULL DEFAULT '',
    dst_port            TEXT NOT NULL DEFAULT '',
    action              TEXT NOT NULL DEFAULT 'ACCEPT',
    gateway_id          TEXT NOT NULL DEFAULT '',
    gateway_group_id    TEXT NOT NULL DEFAULT '',
    fwmark              INTEGER,
    fallback_to_default INTEGER NOT NULL DEFAULT 0,
    enabled             INTEGER NOT NULL DEFAULT 1,
    log                 INTEGER NOT NULL DEFAULT 0,
    comment             TEXT NOT NULL DEFAULT '',
    order_idx           INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT NOT NULL DEFAULT ''
);
`,
	},
	{
		version: 29,
		sql: `
-- Separator color: visual tint for rule group dividers.
-- Empty string = default gray. Values: red|orange|yellow|green|cyan|blue|purple.
-- Separators are always synced to both tables (not part of pending-apply cycle).
ALTER TABLE firewall_rules         ADD COLUMN separator_color TEXT NOT NULL DEFAULT '';
ALTER TABLE firewall_rules_applied ADD COLUMN separator_color TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 30,
		sql: `
-- Remote Cascade servers for multi-server management.
CREATE TABLE IF NOT EXISTS remotes (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	url        TEXT NOT NULL,
	token      TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`,
	},
	{
		version: 31,
		sql: `
-- Speed test results.
CREATE TABLE IF NOT EXISTS speedtest_results (
	id           TEXT PRIMARY KEY,
	from_server  TEXT NOT NULL DEFAULT '',
	to_server    TEXT NOT NULL DEFAULT '',
	host         TEXT NOT NULL DEFAULT '',
	port         INTEGER NOT NULL DEFAULT 0,
	duration     INTEGER NOT NULL DEFAULT 10,
	streams      INTEGER NOT NULL DEFAULT 4,
	status       TEXT NOT NULL DEFAULT 'running',
	send_mbps    REAL,
	recv_mbps    REAL,
	retransmits  INTEGER,
	latency_ms   REAL,
	error        TEXT,
	started_at   TEXT NOT NULL DEFAULT (datetime('now')),
	finished_at  TEXT
);
`,
	},
	{
		version: 32,
		sql: `
ALTER TABLE speedtest_results ADD COLUMN via TEXT NOT NULL DEFAULT 'internet';
`,
	},
	{
		version: 33,
		sql: `
CREATE TABLE IF NOT EXISTS metrics_history (
  ts  INTEGER NOT NULL,
  key TEXT    NOT NULL,
  val REAL    NOT NULL
);
CREATE INDEX IF NOT EXISTS metrics_history_key_ts ON metrics_history(key, ts);
`,
	},
	{
		version: 34,
		sql: `
-- Diagnostics page: separate widget layout stored alongside dashboard.
-- page column defaults to 'dashboard' so existing rows keep working.
ALTER TABLE dashboard_widgets ADD COLUMN page TEXT NOT NULL DEFAULT 'dashboard';
-- Rename the implicit primary key: new PK is (user_id, page).
-- SQLite doesn't support DROP CONSTRAINT, so we recreate the table.
CREATE TABLE IF NOT EXISTS dashboard_widgets_new (
  user_id TEXT NOT NULL,
  page    TEXT NOT NULL DEFAULT 'dashboard',
  widgets TEXT NOT NULL DEFAULT '[]',
  PRIMARY KEY (user_id, page)
);
INSERT INTO dashboard_widgets_new (user_id, page, widgets)
  SELECT user_id, 'dashboard', widgets FROM dashboard_widgets;
DROP TABLE dashboard_widgets;
ALTER TABLE dashboard_widgets_new RENAME TO dashboard_widgets;
`,
	},
	{
		version: 35,
		sql: `
-- metrics_history moved to separate metrics.db file.
-- Drop from cascade.db to free space in config backups.
DROP TABLE IF EXISTS metrics_history;
DROP INDEX IF EXISTS metrics_history_key_ts;
`,
	},
	{
		version: 36,
		sql: `
-- Rate limits on client-group aliases (kbps; 0 = unlimited).
ALTER TABLE aliases ADD COLUMN rate_down INTEGER NOT NULL DEFAULT 0;
ALTER TABLE aliases ADD COLUMN rate_up   INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		version: 37,
		sql: `
ALTER TABLE remotes ADD COLUMN skip_tls_verify INTEGER NOT NULL DEFAULT 0;
`,
	},
	{
		version: 38,
		sql: `
ALTER TABLE templates ADD COLUMN host TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 39,
		sql: `
ALTER TABLE peers ADD COLUMN latest_handshake_at TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 40,
		sql: `
ALTER TABLE interfaces ADD COLUMN dns TEXT NOT NULL DEFAULT '';
`,
	},
}

func runMigrations(db *sql.DB) error {
	// Ensure migrations table exists (bootstraps the migration system itself).
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
	); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Find current schema version.
	var current int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`)
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	// Apply pending migrations in order.
	for _, m := range migrations {
		if m.version <= current {
			continue
		}

		log.Printf("db: applying migration v%d", m.version)

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration v%d: %w", m.version, err)
		}

		if _, err := tx.Exec(m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration v%d: %w", m.version, err)
		}

		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version) VALUES (?)`, m.version,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration v%d: %w", m.version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration v%d: %w", m.version, err)
		}

		log.Printf("db: migration v%d applied", m.version)
	}

	return nil
}
