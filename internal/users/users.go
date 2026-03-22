// Package users manages multi-user authentication data in SQLite.
//
// Users are stored in the `users` table created by migration v7.
// Password hashes use bcrypt cost 12.
// TOTP secrets are stored in plain text (they are already random base32 keys).
// Neither password_hash nor totp_secret is ever exposed in JSON responses.
package users

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/JohnnyVBut/awg-easy/internal/db"
)

// bcryptCost is the work factor for hashing new passwords.
const bcryptCost = 12

// User is the public view of a user record (no sensitive fields).
type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	TOTPEnabled bool      `json:"totp_enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

// ── Read operations ───────────────────────────────────────────────────────────

// List returns all users sorted by created_at ascending.
func List() ([]User, error) {
	rows, err := db.DB().Query(
		`SELECT id, username, totp_enabled, created_at FROM users ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("users.List: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var createdAt string
		if err := rows.Scan(&u.ID, &u.Username, &u.TOTPEnabled, &createdAt); err != nil {
			return nil, fmt.Errorf("users.List scan: %w", err)
		}
		u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		users = append(users, u)
	}
	if users == nil {
		users = []User{}
	}
	return users, rows.Err()
}

// GetByID returns the user with the given ID, or nil if not found.
func GetByID(id string) (*User, error) {
	var u User
	var createdAt string
	err := db.DB().QueryRow(
		`SELECT id, username, totp_enabled, created_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.TOTPEnabled, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("users.GetByID: %w", err)
	}
	u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return &u, nil
}

// GetByUsername returns the user with the given username (case-insensitive), or nil.
func GetByUsername(username string) (*User, error) {
	var u User
	var createdAt string
	err := db.DB().QueryRow(
		`SELECT id, username, totp_enabled, created_at FROM users WHERE username = ? COLLATE NOCASE`,
		username,
	).Scan(&u.ID, &u.Username, &u.TOTPEnabled, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("users.GetByUsername: %w", err)
	}
	u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return &u, nil
}

// Count returns the total number of users in the table.
func Count() (int, error) {
	var n int
	if err := db.DB().QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("users.Count: %w", err)
	}
	return n, nil
}

// ── Write operations ──────────────────────────────────────────────────────────

// Create inserts a new user with a bcrypt-hashed password and returns the created User.
func Create(username, password string) (*User, error) {
	if username == "" {
		return nil, errors.New("username is required")
	}
	if password == "" {
		return nil, errors.New("password is required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("users.Create bcrypt: %w", err)
	}

	id := uuid.New().String()
	_, err = db.DB().Exec(
		`INSERT INTO users (id, username, password_hash) VALUES (?, ?, ?)`,
		id, username, string(hash),
	)
	if err != nil {
		return nil, fmt.Errorf("users.Create insert: %w", err)
	}

	return GetByID(id)
}

// UpdatePassword replaces the password hash for the user with the given ID.
func UpdatePassword(id, newPassword string) error {
	if newPassword == "" {
		return errors.New("password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("users.UpdatePassword bcrypt: %w", err)
	}
	_, err = db.DB().Exec(
		`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id,
	)
	return err
}

// UpdateUsername changes the username for the user with the given ID.
func UpdateUsername(id, newUsername string) error {
	if newUsername == "" {
		return errors.New("username is required")
	}
	_, err := db.DB().Exec(
		`UPDATE users SET username = ? WHERE id = ?`, newUsername, id,
	)
	return err
}

// Delete removes the user with the given ID.
// Returns an error if the user is the last one (cannot delete the last user).
func Delete(id string) error {
	n, err := Count()
	if err != nil {
		return err
	}
	if n <= 1 {
		return errors.New("cannot delete the last user")
	}
	_, err = db.DB().Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// ── Authentication ────────────────────────────────────────────────────────────

// VerifyPassword checks whether the given username/password pair is valid.
// Returns the User on success, nil + error on failure.
func VerifyPassword(username, password string) (*User, error) {
	if username == "" || password == "" {
		return nil, errors.New("invalid credentials")
	}

	var id, hash, createdAt string
	var totpEnabled bool
	err := db.DB().QueryRow(
		`SELECT id, username, password_hash, totp_enabled, created_at
		   FROM users WHERE username = ? COLLATE NOCASE`,
		username,
	).Scan(&id, &username, &hash, &totpEnabled, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("invalid credentials")
	}
	if err != nil {
		return nil, fmt.Errorf("users.VerifyPassword query: %w", err)
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, errors.New("invalid credentials")
	}

	u := &User{ID: id, Username: username, TOTPEnabled: totpEnabled}
	u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return u, nil
}

// ── TOTP operations ───────────────────────────────────────────────────────────

// GetTOTPSecret returns the stored TOTP secret for the user.
// Returns an empty string if no secret is set.
func GetTOTPSecret(userID string) (string, error) {
	var secret string
	err := db.DB().QueryRow(
		`SELECT totp_secret FROM users WHERE id = ?`, userID,
	).Scan(&secret)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("users.GetTOTPSecret: %w", err)
	}
	return secret, nil
}

// SetTOTP saves the TOTP secret and marks totp_enabled = 1.
func SetTOTP(userID, secret string) error {
	_, err := db.DB().Exec(
		`UPDATE users SET totp_secret = ?, totp_enabled = 1 WHERE id = ?`,
		secret, userID,
	)
	return err
}

// ClearTOTP removes the TOTP secret and marks totp_enabled = 0.
func ClearTOTP(userID string) error {
	_, err := db.DB().Exec(
		`UPDATE users SET totp_secret = '', totp_enabled = 0 WHERE id = ?`,
		userID,
	)
	return err
}

// IsTOTPEnabled returns true if totp_enabled = 1 for the given user.
func IsTOTPEnabled(userID string) (bool, error) {
	var enabled bool
	err := db.DB().QueryRow(
		`SELECT totp_enabled FROM users WHERE id = ?`, userID,
	).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("users.IsTOTPEnabled: %w", err)
	}
	return enabled, nil
}

// ── Startup seeding ───────────────────────────────────────────────────────────

// SeedAdminIfEmpty creates an "admin" user with the given bcrypt password hash
// if the users table is empty.  Called once at startup so that an existing
// PASSWORD_HASH env-var keeps working after the migration to multi-user auth.
func SeedAdminIfEmpty(passwordHash string) error {
	n, err := Count()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil // table already has users — nothing to do
	}
	if passwordHash == "" {
		return nil // no seed hash provided — run in open mode
	}

	id := uuid.New().String()
	_, err = db.DB().Exec(
		`INSERT INTO users (id, username, password_hash) VALUES (?, 'admin', ?)`,
		id, passwordHash,
	)
	if err != nil {
		return fmt.Errorf("users.SeedAdminIfEmpty: %w", err)
	}
	return nil
}
