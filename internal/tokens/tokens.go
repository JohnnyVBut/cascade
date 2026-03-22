// Package tokens manages API tokens for programmatic access.
//
// Each token is a 67-character random string prefixed with "ws_".
// Only the SHA-256 hash is stored in the database — the raw token value
// is returned once at creation time and cannot be retrieved later.
//
// Token format: "ws_" + 64 hex chars (32 random bytes).
// Storage: SHA-256(raw_token) stored as hex string — fast lookup, no bcrypt needed
// (tokens are 256 bits of entropy, rainbow tables are not a concern).
package tokens

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/JohnnyVBut/cascade/internal/db"
)

// tokenPrefix is prepended to all raw tokens for easy identification.
const tokenPrefix = "ws_"

// Token is the public view of an API token (no raw value, no hash).
type Token struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Name      string     `json:"name"`
	LastUsed  *time.Time `json:"last_used"`
	CreatedAt time.Time  `json:"created_at"`
}

// generate creates a new random token value and its SHA-256 hash.
func generate() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("tokens.generate: %w", err)
	}
	raw = tokenPrefix + hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(sum[:])
	return raw, hash, nil
}

// Create generates a new token, stores its hash in the DB, and returns both
// the Token record and the raw token value. The raw value is shown ONCE —
// it is not stored and cannot be retrieved later.
func Create(userID, name string) (*Token, string, error) {
	if userID == "" {
		return nil, "", errors.New("userID is required")
	}
	if name == "" {
		return nil, "", errors.New("name is required")
	}

	raw, hash, err := generate()
	if err != nil {
		return nil, "", err
	}

	id := uuid.New().String()
	_, err = db.DB().Exec(
		`INSERT INTO api_tokens (id, user_id, name, token_hash) VALUES (?, ?, ?, ?)`,
		id, userID, name, hash,
	)
	if err != nil {
		return nil, "", fmt.Errorf("tokens.Create: %w", err)
	}

	t, err := GetByID(id)
	if err != nil {
		return nil, "", err
	}
	return t, raw, nil
}

// ListByUser returns all tokens for a given user, sorted by created_at ascending.
func ListByUser(userID string) ([]Token, error) {
	rows, err := db.DB().Query(
		`SELECT id, user_id, name, last_used, created_at
		   FROM api_tokens WHERE user_id = ? ORDER BY created_at ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("tokens.ListByUser: %w", err)
	}
	defer rows.Close()

	var result []Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *t)
	}
	if result == nil {
		result = []Token{}
	}
	return result, rows.Err()
}

// GetByID returns the token with the given ID, or nil if not found.
func GetByID(id string) (*Token, error) {
	row := db.DB().QueryRow(
		`SELECT id, user_id, name, last_used, created_at FROM api_tokens WHERE id = ?`, id,
	)
	t, err := scanToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// Delete removes a token by ID, verifying it belongs to the given user.
func Delete(id, userID string) error {
	res, err := db.DB().Exec(
		`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID,
	)
	if err != nil {
		return fmt.Errorf("tokens.Delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("token not found")
	}
	return nil
}

// VerifyAndTouch looks up a token by its raw value, updates last_used,
// and returns the owning user's ID. Returns ("", error) if invalid.
func VerifyAndTouch(raw string) (string, error) {
	if !strings.HasPrefix(raw, tokenPrefix) {
		return "", errors.New("invalid token format")
	}
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])

	var userID string
	err := db.DB().QueryRow(
		`SELECT user_id FROM api_tokens WHERE token_hash = ?`, hash,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("token not found")
	}
	if err != nil {
		return "", fmt.Errorf("tokens.VerifyAndTouch: %w", err)
	}

	// Update last_used — best-effort, never blocks the request.
	_, _ = db.DB().Exec(
		`UPDATE api_tokens SET last_used = datetime('now') WHERE token_hash = ?`, hash,
	)

	return userID, nil
}

// ── scanner helpers ───────────────────────────────────────────────────────────

type rowScanner interface {
	Scan(dest ...any) error
}

func scanToken(s rowScanner) (*Token, error) {
	var t Token
	var lastUsed sql.NullString
	var createdAt string
	if err := s.Scan(&t.ID, &t.UserID, &t.Name, &lastUsed, &createdAt); err != nil {
		return nil, fmt.Errorf("tokens.scanToken: %w", err)
	}
	t.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	if lastUsed.Valid && lastUsed.String != "" {
		lu, _ := time.Parse("2006-01-02 15:04:05", lastUsed.String)
		t.LastUsed = &lu
	}
	return &t, nil
}
