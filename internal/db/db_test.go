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

	// Current latest migration is v9 (is_admin column on users).
	want := len(migrations) // should equal 9
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
