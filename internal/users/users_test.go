// Package users — unit tests for admin role and privilege escalation fix (MED-3).
//
// All tests use an in-memory SQLite database initialized via db.Init().
// Each test function reinitializes the DB from a fresh temp directory to
// ensure test isolation.
package users

import (
	"os"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
)

// initTestDB creates a fresh temp directory, calls db.Init(), and registers a
// cleanup function to close the DB and remove the directory.
func initTestDB(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cascade-users-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
}

// ── CountAdmins ───────────────────────────────────────────────────────────────

func TestCountAdmins_ZeroWhenEmpty(t *testing.T) {
	initTestDB(t)

	n, err := CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 admins in empty table, got %d", n)
	}
}

func TestCountAdmins_OneAfterSeed(t *testing.T) {
	initTestDB(t)

	// Use a bcrypt hash of "password" generated at cost 12.
	// We use a pre-computed hash to avoid the cost of bcrypt in tests.
	// $2a$04$... — cost 4 for speed in unit tests.
	hash := "$2a$04$NpJMnalrDU8yFBbKWFMXrumYRZzEEiD9uq0UFXilFCJJCAtpAv/bm" // "password"

	if err := SeedAdminIfEmpty(hash); err != nil {
		t.Fatalf("SeedAdminIfEmpty: %v", err)
	}

	n, err := CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 admin after SeedAdminIfEmpty, got %d", n)
	}
}

// ── IsAdmin ───────────────────────────────────────────────────────────────────

func TestIsAdmin_TrueForSeededAdmin(t *testing.T) {
	initTestDB(t)

	hash := "$2a$04$NpJMnalrDU8yFBbKWFMXrumYRZzEEiD9uq0UFXilFCJJCAtpAv/bm" // "password"
	if err := SeedAdminIfEmpty(hash); err != nil {
		t.Fatalf("SeedAdminIfEmpty: %v", err)
	}

	u, err := GetByUsername("admin")
	if err != nil || u == nil {
		t.Fatalf("GetByUsername(admin): %v, %v", u, err)
	}

	admin, err := IsAdmin(u.ID)
	if err != nil {
		t.Fatalf("IsAdmin: %v", err)
	}
	if !admin {
		t.Error("expected seeded 'admin' user to have is_admin=true")
	}
}

func TestIsAdmin_FalseForRegularUser(t *testing.T) {
	initTestDB(t)

	// Seed admin first so we have at least 1 user.
	hash := "$2a$04$NpJMnalrDU8yFBbKWFMXrumYRZzEEiD9uq0UFXilFCJJCAtpAv/bm"
	if err := SeedAdminIfEmpty(hash); err != nil {
		t.Fatalf("SeedAdminIfEmpty: %v", err)
	}

	// Create a regular user.
	regular, err := Create("regular", "pass1234")
	if err != nil {
		t.Fatalf("Create regular user: %v", err)
	}

	admin, err := IsAdmin(regular.ID)
	if err != nil {
		t.Fatalf("IsAdmin: %v", err)
	}
	if admin {
		t.Error("expected newly created user to have is_admin=false")
	}
}

func TestIsAdmin_FalseForNonExistentUser(t *testing.T) {
	initTestDB(t)

	admin, err := IsAdmin("non-existent-id")
	if err != nil {
		t.Fatalf("IsAdmin for non-existent ID: %v", err)
	}
	if admin {
		t.Error("expected IsAdmin to return false for non-existent user")
	}
}

// ── SetAdmin ──────────────────────────────────────────────────────────────────

func TestSetAdmin_FailsWhenOnlyOneAdmin(t *testing.T) {
	initTestDB(t)

	hash := "$2a$04$NpJMnalrDU8yFBbKWFMXrumYRZzEEiD9uq0UFXilFCJJCAtpAv/bm"
	if err := SeedAdminIfEmpty(hash); err != nil {
		t.Fatalf("SeedAdminIfEmpty: %v", err)
	}

	u, err := GetByUsername("admin")
	if err != nil || u == nil {
		t.Fatalf("GetByUsername: %v", err)
	}

	// Attempt to remove admin from the only admin — must fail.
	err = SetAdmin(u.ID, false)
	if err == nil {
		t.Error("expected SetAdmin(false) to fail when there is only 1 admin, got nil")
	}
}

func TestSetAdmin_SucceedsWhenTwoAdmins(t *testing.T) {
	initTestDB(t)

	hash := "$2a$04$NpJMnalrDU8yFBbKWFMXrumYRZzEEiD9uq0UFXilFCJJCAtpAv/bm"
	if err := SeedAdminIfEmpty(hash); err != nil {
		t.Fatalf("SeedAdminIfEmpty: %v", err)
	}

	// Create a second user and promote them to admin.
	second, err := Create("second", "pass5678")
	if err != nil {
		t.Fatalf("Create second user: %v", err)
	}
	if err := SetAdmin(second.ID, true); err != nil {
		t.Fatalf("SetAdmin(true) for second user: %v", err)
	}

	// Now there are 2 admins — removing one should succeed.
	u, _ := GetByUsername("admin")
	if err := SetAdmin(u.ID, false); err != nil {
		t.Errorf("SetAdmin(false) should succeed when 2 admins exist, got: %v", err)
	}

	// Confirm the flag was cleared.
	admin, _ := IsAdmin(u.ID)
	if admin {
		t.Error("expected is_admin to be false after SetAdmin(false)")
	}
}

func TestSetAdmin_GrantAndVerify(t *testing.T) {
	initTestDB(t)

	hash := "$2a$04$NpJMnalrDU8yFBbKWFMXrumYRZzEEiD9uq0UFXilFCJJCAtpAv/bm"
	if err := SeedAdminIfEmpty(hash); err != nil {
		t.Fatalf("SeedAdminIfEmpty: %v", err)
	}

	regular, err := Create("newguy", "abc12345")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Initially not admin.
	admin, _ := IsAdmin(regular.ID)
	if admin {
		t.Fatal("new user should not be admin before SetAdmin(true)")
	}

	// Grant admin.
	if err := SetAdmin(regular.ID, true); err != nil {
		t.Fatalf("SetAdmin(true): %v", err)
	}

	admin, _ = IsAdmin(regular.ID)
	if !admin {
		t.Error("expected is_admin=true after SetAdmin(true)")
	}
}

// ── SeedAdminIfEmpty ──────────────────────────────────────────────────────────

func TestSeedAdminIfEmpty_CreatesIsAdminTrue(t *testing.T) {
	initTestDB(t)

	hash := "$2a$04$NpJMnalrDU8yFBbKWFMXrumYRZzEEiD9uq0UFXilFCJJCAtpAv/bm"
	if err := SeedAdminIfEmpty(hash); err != nil {
		t.Fatalf("SeedAdminIfEmpty: %v", err)
	}

	u, err := GetByUsername("admin")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if u == nil {
		t.Fatal("expected admin user to be created")
	}
	if !u.IsAdmin {
		t.Error("SeedAdminIfEmpty should create user with IsAdmin=true")
	}
}

func TestSeedAdminIfEmpty_DoesNotSeedWhenUsersExist(t *testing.T) {
	initTestDB(t)

	// Create a user first.
	if _, err := Create("existing", "somepassword"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	hash := "$2a$04$NpJMnalrDU8yFBbKWFMXrumYRZzEEiD9uq0UFXilFCJJCAtpAv/bm"
	if err := SeedAdminIfEmpty(hash); err != nil {
		t.Fatalf("SeedAdminIfEmpty: %v", err)
	}

	// Only the original user should exist.
	n, _ := Count()
	if n != 1 {
		t.Errorf("expected 1 user (no seeding when table non-empty), got %d", n)
	}
}

func TestSeedAdminIfEmpty_NoOpWhenHashEmpty(t *testing.T) {
	initTestDB(t)

	if err := SeedAdminIfEmpty(""); err != nil {
		t.Fatalf("SeedAdminIfEmpty with empty hash: %v", err)
	}

	n, _ := Count()
	if n != 0 {
		t.Errorf("expected 0 users when hash is empty, got %d", n)
	}
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestCreate_DefaultIsAdminFalse(t *testing.T) {
	initTestDB(t)

	// Seed admin so the table is non-empty (matching real usage).
	hash := "$2a$04$NpJMnalrDU8yFBbKWFMXrumYRZzEEiD9uq0UFXilFCJJCAtpAv/bm"
	if err := SeedAdminIfEmpty(hash); err != nil {
		t.Fatalf("SeedAdminIfEmpty: %v", err)
	}

	u, err := Create("bob", "hunter2")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if u.IsAdmin {
		t.Error("Create() should set is_admin=false for regular users")
	}

	// Confirm via direct DB query.
	admin, err := IsAdmin(u.ID)
	if err != nil {
		t.Fatalf("IsAdmin: %v", err)
	}
	if admin {
		t.Error("is_admin should be 0/false in DB after Create()")
	}
}

// ── CountAdmins with multiple users ───────────────────────────────────────────

func TestCountAdmins_MultipleUsersOnlyOneAdmin(t *testing.T) {
	initTestDB(t)

	hash := "$2a$04$NpJMnalrDU8yFBbKWFMXrumYRZzEEiD9uq0UFXilFCJJCAtpAv/bm"
	if err := SeedAdminIfEmpty(hash); err != nil {
		t.Fatalf("SeedAdminIfEmpty: %v", err)
	}

	// Add two regular users.
	if _, err := Create("alice", "alicepass"); err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	if _, err := Create("bob", "bobpass"); err != nil {
		t.Fatalf("Create bob: %v", err)
	}

	n, err := CountAdmins()
	if err != nil {
		t.Fatalf("CountAdmins: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 admin with 3 total users, got %d", n)
	}
}
