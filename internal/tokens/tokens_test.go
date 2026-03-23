package tokens

import (
	"os"
	"strings"
	"testing"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/users"
)

func initTestDB(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cascade-tokens-test-*")
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

// seedUser creates a test user and returns its ID.
func seedUser(t *testing.T, username string) string {
	t.Helper()
	u, err := users.Create(username, "testpassword")
	if err != nil {
		t.Fatalf("users.Create(%q): %v", username, err)
	}
	return u.ID
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestCreate_ReturnsTokenWithRawValue(t *testing.T) {
	initTestDB(t)
	userID := seedUser(t, "alice")

	tok, raw, err := Create(userID, "my-api-key")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tok == nil {
		t.Fatal("expected non-nil Token")
	}
	if raw == "" {
		t.Fatal("expected non-empty raw token")
	}
	if !strings.HasPrefix(raw, "ws_") {
		t.Errorf("raw token should start with 'ws_', got %q", raw)
	}
	if tok.ID == "" {
		t.Error("Token.ID should not be empty")
	}
	if tok.UserID != userID {
		t.Errorf("Token.UserID = %q, want %q", tok.UserID, userID)
	}
	if tok.Name != "my-api-key" {
		t.Errorf("Token.Name = %q, want 'my-api-key'", tok.Name)
	}
}

func TestCreate_EmptyUserID_Error(t *testing.T) {
	initTestDB(t)
	_, _, err := Create("", "test")
	if err == nil {
		t.Error("expected error for empty userID, got nil")
	}
}

func TestCreate_EmptyName_Error(t *testing.T) {
	initTestDB(t)
	userID := seedUser(t, "bob")
	_, _, err := Create(userID, "")
	if err == nil {
		t.Error("expected error for empty name, got nil")
	}
}

// ── GetByID ───────────────────────────────────────────────────────────────────

func TestGetByID_ExistingToken(t *testing.T) {
	initTestDB(t)
	userID := seedUser(t, "charlie")

	tok, _, err := Create(userID, "my-token")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := GetByID(tok.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil Token")
	}
	if got.ID != tok.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, tok.ID)
	}
	if got.Name != "my-token" {
		t.Errorf("Name mismatch: got %q, want 'my-token'", got.Name)
	}
}

func TestGetByID_NotFound_ReturnsNil(t *testing.T) {
	initTestDB(t)
	got, err := GetByID("non-existent-id")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for unknown ID, got %+v", got)
	}
}

// ── ListByUser ────────────────────────────────────────────────────────────────

func TestListByUser_ReturnsCreatedTokens(t *testing.T) {
	initTestDB(t)
	userID := seedUser(t, "dave")

	_, _, err := Create(userID, "token-one")
	if err != nil {
		t.Fatalf("Create token-one: %v", err)
	}
	_, _, err = Create(userID, "token-two")
	if err != nil {
		t.Fatalf("Create token-two: %v", err)
	}

	list, err := ListByUser(userID)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(list))
	}
}

func TestListByUser_Empty_ReturnsEmptySlice(t *testing.T) {
	initTestDB(t)
	userID := seedUser(t, "eve")

	list, err := ListByUser(userID)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if list == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(list) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(list))
	}
}

func TestListByUser_IsolatedByUser(t *testing.T) {
	initTestDB(t)
	user1 := seedUser(t, "frank")
	user2 := seedUser(t, "grace")

	Create(user1, "t1")
	Create(user1, "t2")
	Create(user2, "t3")

	list1, _ := ListByUser(user1)
	list2, _ := ListByUser(user2)

	if len(list1) != 2 {
		t.Errorf("user1: expected 2 tokens, got %d", len(list1))
	}
	if len(list2) != 1 {
		t.Errorf("user2: expected 1 token, got %d", len(list2))
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDelete_RemovesToken(t *testing.T) {
	initTestDB(t)
	userID := seedUser(t, "henry")

	tok, _, err := Create(userID, "to-delete")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := Delete(tok.ID, userID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := GetByID(tok.ID)
	if err != nil {
		t.Fatalf("GetByID after delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after Delete, got %+v", got)
	}
}

func TestDelete_WrongUser_Error(t *testing.T) {
	initTestDB(t)
	user1 := seedUser(t, "ivan")
	user2 := seedUser(t, "judy")

	tok, _, _ := Create(user1, "owned-by-user1")

	err := Delete(tok.ID, user2)
	if err == nil {
		t.Error("expected error when deleting another user's token, got nil")
	}
}

func TestDelete_NonExistent_Error(t *testing.T) {
	initTestDB(t)
	userID := seedUser(t, "kate")

	err := Delete("no-such-id", userID)
	if err == nil {
		t.Error("expected error for non-existent token ID, got nil")
	}
}

// ── VerifyAndTouch ────────────────────────────────────────────────────────────

func TestVerifyAndTouch_ValidToken(t *testing.T) {
	initTestDB(t)
	userID := seedUser(t, "leo")

	_, raw, err := Create(userID, "verify-me")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	gotUserID, err := VerifyAndTouch(raw)
	if err != nil {
		t.Fatalf("VerifyAndTouch: %v", err)
	}
	if gotUserID != userID {
		t.Errorf("VerifyAndTouch returned userID=%q, want %q", gotUserID, userID)
	}
}

func TestVerifyAndTouch_InvalidPrefix_Error(t *testing.T) {
	initTestDB(t)
	_, err := VerifyAndTouch("no_prefix_here_00000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Error("expected error for token without 'ws_' prefix, got nil")
	}
}

func TestVerifyAndTouch_UnknownToken_Error(t *testing.T) {
	initTestDB(t)
	_, err := VerifyAndTouch("ws_" + strings.Repeat("a", 64))
	if err == nil {
		t.Error("expected error for unknown token, got nil")
	}
}
