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
