// Package remotes manages remote Cascade server records.
//
// A "remote" is another Cascade instance that this server can manage via its API.
// The remote's API token (ws_...) is stored plain-text in SQLite — it is obtained
// at add-time by logging in with username/password, creating a token, then logging out.
// The password itself is never persisted.
package remotes

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/JohnnyVBut/cascade/internal/db"
)

// Remote represents a registered remote Cascade server.
type Remote struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	URL           string    `json:"url"`
	Token         string    `json:"token,omitempty"` // omitted in list responses
	SkipTLSVerify bool      `json:"skipTlsVerify"`
	CreatedAt     time.Time `json:"createdAt"`
}

// List returns all registered remotes ordered by name.
func List() ([]*Remote, error) {
	rows, err := db.DB().Query(`
		SELECT id, name, url, skip_tls_verify, created_at
		FROM remotes
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("remotes list: %w", err)
	}
	defer rows.Close()

	var out []*Remote
	for rows.Next() {
		r := &Remote{}
		var createdAt string
		var skipTLS int
		if err := rows.Scan(&r.ID, &r.Name, &r.URL, &skipTLS, &createdAt); err != nil {
			return nil, fmt.Errorf("remotes scan: %w", err)
		}
		r.SkipTLSVerify = skipTLS != 0
		r.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		out = append(out, r)
	}
	if out == nil {
		out = []*Remote{}
	}
	return out, nil
}

// Get returns a single remote by ID, including the token (for internal use).
func Get(id string) (*Remote, error) {
	r := &Remote{}
	var createdAt string
	var skipTLS int
	err := db.DB().QueryRow(`
		SELECT id, name, url, token, skip_tls_verify, created_at
		FROM remotes WHERE id = ?
	`, id).Scan(&r.ID, &r.Name, &r.URL, &r.Token, &skipTLS, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("remotes get %q: %w", id, err)
	}
	r.SkipTLSVerify = skipTLS != 0
	r.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	return r, nil
}

// Add inserts a new remote and returns it (without token in the response).
func Add(name, url, token string, skipTLSVerify bool) (*Remote, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if url == "" {
		return nil, fmt.Errorf("url is required")
	}
	if token == "" {
		return nil, fmt.Errorf("token is required")
	}

	skipTLS := 0
	if skipTLSVerify {
		skipTLS = 1
	}
	r := &Remote{
		ID:            uuid.New().String(),
		Name:          name,
		URL:           url,
		SkipTLSVerify: skipTLSVerify,
		CreatedAt:     time.Now().UTC(),
	}
	_, err := db.DB().Exec(`
		INSERT INTO remotes (id, name, url, token, skip_tls_verify, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, r.ID, r.Name, r.URL, token, skipTLS, r.CreatedAt.Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, fmt.Errorf("remotes add: %w", err)
	}
	return r, nil
}

// Delete removes a remote by ID.
func Delete(id string) error {
	res, err := db.DB().Exec(`DELETE FROM remotes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("remotes delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("remote %q not found", id)
	}
	return nil
}
