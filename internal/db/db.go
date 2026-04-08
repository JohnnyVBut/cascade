// Package db manages the SQLite database lifecycle.
//
// Single file: <dataDir>/wireguard.db
// Backup: cp wireguard.db wireguard.db.bak
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
	"path/filepath"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

var instance *sql.DB

// Init opens (or creates) wireguard.db in dataDir and runs all pending migrations.
// Must be called once at startup before any other package uses DB().
func Init(dataDir string) error {
	path := filepath.Join(dataDir, "wireguard.db")

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
	return nil
}

// DB returns the global database handle.
// Panics if Init() has not been called.
func DB() *sql.DB {
	if instance == nil {
		panic("db.Init() must be called before db.DB()")
	}
	return instance
}

// Close closes the database. Call on graceful shutdown.
func Close() {
	if instance != nil {
		instance.Close()
		instance = nil
	}
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
