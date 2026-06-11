// Package remoteclient provides an HTTP client for interacting with a remote
// Cascade server.
//
// ObtainToken performs the full login → create-token → logout flow against
// the remote server and returns the raw API token (ws_...) to be stored locally.
// The password is never persisted — only the resulting token is saved.
package remoteclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// httpClient is shared across all calls with a reasonable timeout.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// ObtainToken authenticates against a remote Cascade server using username and
// password, creates an API token, logs out, and returns the raw token string.
func ObtainToken(baseURL, username, password string) (string, error) {
	base := strings.TrimRight(baseURL, "/")

	// ── Step 1: Login ──────────────────────────────────────────────────────
	loginBody, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	loginResp, err := httpClient.Post(base+"/api/session", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		return "", fmt.Errorf("login request: %w", err)
	}
	defer loginResp.Body.Close()

	if loginResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(loginResp.Body)
		return "", fmt.Errorf("login failed (%d): %s", loginResp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Parse login response — check for TOTP requirement.
	var loginData map[string]any
	if err := json.NewDecoder(loginResp.Body).Decode(&loginData); err != nil {
		return "", fmt.Errorf("parse login response: %w", err)
	}
	if req, _ := loginData["totp_required"].(bool); req {
		return "", fmt.Errorf("remote server requires TOTP — disable 2FA on the remote or use an existing API token")
	}
	if auth, _ := loginData["authenticated"].(bool); !auth {
		return "", fmt.Errorf("login did not return authenticated=true")
	}

	// Extract session cookie for subsequent requests.
	var sessionCookie string
	for _, c := range loginResp.Cookies() {
		if c.Name == "session_id" {
			sessionCookie = c.Name + "=" + c.Value
			break
		}
	}
	if sessionCookie == "" {
		return "", fmt.Errorf("no session_id cookie in login response")
	}

	// ── Step 2: Create API token ───────────────────────────────────────────
	tokenBody, _ := json.Marshal(map[string]string{
		"name": "cascade-remote",
	})
	tokenReq, _ := http.NewRequest(http.MethodPost, base+"/api/tokens", bytes.NewReader(tokenBody))
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenReq.Header.Set("Cookie", sessionCookie)

	tokenResp, err := httpClient.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(tokenResp.Body)
		return "", fmt.Errorf("create token failed (%d): %s", tokenResp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenData map[string]any
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}
	rawToken, _ := tokenData["raw_token"].(string)
	if rawToken == "" {
		return "", fmt.Errorf("raw_token missing in token creation response")
	}

	// ── Step 3: Logout ─────────────────────────────────────────────────────
	logoutReq, _ := http.NewRequest(http.MethodDelete, base+"/api/session", nil)
	logoutReq.Header.Set("Cookie", sessionCookie)
	resp, err := httpClient.Do(logoutReq)
	if err == nil {
		resp.Body.Close()
	}

	return rawToken, nil
}

// Ping checks whether the remote server is reachable and the token is valid.
// Returns nil if healthy.
func Ping(baseURL, token string) error {
	base := strings.TrimRight(baseURL, "/")
	req, _ := http.NewRequest(http.MethodGet, base+"/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping returned %d", resp.StatusCode)
	}
	return nil
}
