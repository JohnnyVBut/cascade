package db

import (
	"os"
	"testing"
)

func TestOpen_CreatesDBFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-db-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	t.Cleanup(func() {
		Close()
		os.RemoveAll(dir)
	})

	if err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// DB handle must be non-nil.
	d := DB()
	if d == nil {
		t.Fatal("DB() returned nil after Init()")
	}

	// Ping to confirm connection works.
	if err := d.Ping(); err != nil {
		t.Fatalf("DB().Ping(): %v", err)
	}
}

// ── Table existence checks ────────────────────────────────────────────────────

// expectedTables is the set of tables created by migrations v1-v9.
var expectedTables = []string{
	"settings",
	"templates",
	"interfaces",
	"peers",
	"routes",
	"nat_rules",
	"aliases",
	"firewall_rules",
	"gateways",
	"gateway_groups",
	"schema_migrations",
	"users",
	"api_tokens",
}

func TestOpen_AllTablesExist(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-db-tables-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	t.Cleanup(func() {
		Close()
		os.RemoveAll(dir)
	})

	if err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}

	d := DB()
	for _, table := range expectedTables {
		t.Run(table, func(t *testing.T) {
			var name string
			err := d.QueryRow(
				`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
			).Scan(&name)
			if err != nil {
				t.Errorf("table %q not found in sqlite_master: %v", table, err)
			} else if name != table {
				t.Errorf("expected table name %q, got %q", table, name)
			}
		})
	}
}

// ── Migration version ─────────────────────────────────────────────────────────

func TestOpen_MigrationVersionIsLatest(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-db-migver-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	t.Cleanup(func() {
		Close()
		os.RemoveAll(dir)
	})

	if err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}

	d := DB()
	var version int
	if err := d.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}

	// Current latest migration is v11 (traffic accumulation columns on peers).
	want := len(migrations) // should equal 11
	if version != want {
		t.Errorf("schema version = %d, want %d", version, want)
	}
}

// ── Idempotency: Init twice (same dir) ────────────────────────────────────────

func TestOpen_IdempotentMigrations(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-db-idem-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	t.Cleanup(func() {
		Close()
		os.RemoveAll(dir)
	})

	if err := Init(dir); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	Close()

	// Second Init should not fail (migrations already applied).
	if err := Init(dir); err != nil {
		t.Fatalf("second Init (idempotent): %v", err)
	}
}

// ── DB() panics before Init ───────────────────────────────────────────────────

func TestDB_PanicsBeforeInit(t *testing.T) {
	// Ensure no DB is open (Close is safe to call even when already nil).
	Close()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from DB() before Init(), got none")
		}
	}()
	DB()
}

// ── Migration v10: grant admin to first user if none exists ───────────────────

// v10SQL is the exact SQL from migration v10 — tested in isolation below.
const v10SQL = `
UPDATE users SET is_admin = 1
WHERE id = (SELECT id FROM users ORDER BY created_at ASC LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM users WHERE is_admin = 1);
`

// TestMigration_v10_GrantsAdminToFirstUser verifies that the v10 migration SQL
// promotes the oldest user to admin when no admin exists yet.
func TestMigration_v10_GrantsAdminToFirstUser(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-db-v10-grant-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	t.Cleanup(func() {
		Close()
		os.RemoveAll(dir)
	})

	// Migrations run automatically via Init() — fresh DB is at the latest version.
	if err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}

	d := DB()

	// Insert a user directly with is_admin=0, simulating a pre-v10 state
	// where no admin was assigned.
	_, err = d.Exec(
		`INSERT INTO users (id, username, password_hash, is_admin) VALUES ('u1', 'testuser', 'hash', 0)`,
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// Simulate the v10 migration by running its SQL directly.
	if _, err := d.Exec(v10SQL); err != nil {
		t.Fatalf("v10 SQL: %v", err)
	}

	// The user should now have is_admin=1.
	var isAdmin int
	if err := d.QueryRow(`SELECT is_admin FROM users WHERE id = 'u1'`).Scan(&isAdmin); err != nil {
		t.Fatalf("query is_admin: %v", err)
	}
	if isAdmin != 1 {
		t.Errorf("expected is_admin=1 after v10 migration, got %d", isAdmin)
	}
}

// TestMigration_v10_IdempotentWhenAdminExists verifies that the v10 migration SQL
// does not touch any user when at least one admin already exists.
func TestMigration_v10_IdempotentWhenAdminExists(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-db-v10-idem-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	t.Cleanup(func() {
		Close()
		os.RemoveAll(dir)
	})

	if err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}

	d := DB()

	// Insert first user with is_admin=1 (already an admin).
	_, err = d.Exec(
		`INSERT INTO users (id, username, password_hash, is_admin) VALUES ('u1', 'firstuser', 'hash', 1)`,
	)
	if err != nil {
		t.Fatalf("insert first user: %v", err)
	}

	// Insert second user with is_admin=0.
	_, err = d.Exec(
		`INSERT INTO users (id, username, password_hash, is_admin) VALUES ('u2', 'seconduser', 'hash', 0)`,
	)
	if err != nil {
		t.Fatalf("insert second user: %v", err)
	}

	// Simulate the v10 migration by running its SQL directly.
	if _, err := d.Exec(v10SQL); err != nil {
		t.Fatalf("v10 SQL: %v", err)
	}

	// Second user must still have is_admin=0 — the migration is idempotent.
	var isAdmin int
	if err := d.QueryRow(`SELECT is_admin FROM users WHERE id = 'u2'`).Scan(&isAdmin); err != nil {
		t.Fatalf("query is_admin for u2: %v", err)
	}
	if isAdmin != 0 {
		t.Errorf("expected is_admin=0 for second user after v10 migration (admin already existed), got %d", isAdmin)
	}
}

// ── Migration v11: traffic accumulation columns ───────────────────────────────

// TestMigration_v11_TrafficColumnsExist verifies that migration v11 adds
// total_rx and total_tx columns to the peers table with correct defaults.
func TestMigration_v11_TrafficColumnsExist(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-db-v11-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := Init(dir); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()

	d := DB()

	// Insert a minimal interface row (required for the peers FK).
	if _, err := d.Exec(`
		INSERT INTO interfaces (id, name, address, listen_port, protocol, enabled,
		                        disable_routes, private_key, public_key, created_at)
		VALUES ('wg10','test','10.8.0.1/24',51830,'wireguard-1.0',0,0,'priv','pub',datetime('now'))
	`); err != nil {
		t.Fatalf("insert interface: %v", err)
	}

	// Insert a minimal peer row — total_rx/total_tx should default to 0.
	if _, err := d.Exec(`
		INSERT INTO peers (id, interface_id, name, public_key, allowed_ips)
		VALUES ('p1','wg10','alice','AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=','10.8.0.2/32')
	`); err != nil {
		t.Fatalf("insert peer: %v", err)
	}

	var totalRx, totalTx int64
	if err := d.QueryRow(`SELECT total_rx, total_tx FROM peers WHERE id='p1'`).Scan(&totalRx, &totalTx); err != nil {
		t.Fatalf("query total_rx/total_tx (columns may be missing): %v", err)
	}
	if totalRx != 0 {
		t.Errorf("default total_rx = %d, want 0", totalRx)
	}
	if totalTx != 0 {
		t.Errorf("default total_tx = %d, want 0", totalTx)
	}

	// Verify the columns can be updated.
	if _, err := d.Exec(`UPDATE peers SET total_rx=1024, total_tx=2048 WHERE id='p1'`); err != nil {
		t.Fatalf("update total_rx/total_tx: %v", err)
	}
	if err := d.QueryRow(`SELECT total_rx, total_tx FROM peers WHERE id='p1'`).Scan(&totalRx, &totalTx); err != nil {
		t.Fatalf("re-query: %v", err)
	}
	if totalRx != 1024 || totalTx != 2048 {
		t.Errorf("updated values: got rx=%d tx=%d, want 1024/2048", totalRx, totalTx)
	}
}
