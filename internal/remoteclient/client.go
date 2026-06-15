// Package remoteclient provides an HTTP client for interacting with a remote
// Cascade server.
//
// ObtainToken performs the full login → (TOTP verify) → create-token → logout
// flow against the remote server and returns the raw API token (ws_...) to be
// stored locally. The password is never persisted — only the resulting token is
// saved.
package remoteclient

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrTOTPRequired is returned by ObtainToken when the remote server requires
// a TOTP code but none was provided. The caller should ask the user for the
// code and retry with it.
var ErrTOTPRequired = errors.New("totp_required")

// httpClient is shared across all calls with a reasonable timeout.
// Its transport uses SafeDialContext so every connection to a remote re-checks
// the resolved IP at dial time — protecting Ping/ObtainToken against SSRF via
// DNS rebinding (see ssrf.go).
var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		DialContext: SafeDialContext,
	},
	// Never follow redirects from a remote server. A redirect to an internal
	// address would bypass the SSRF guard on the original request.
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// clientFor returns httpClient, or a one-off client with TLS verification
// disabled when skipTLS is true (for remotes with self-signed certificates).
func clientFor(skipTLS bool) *http.Client {
	if !skipTLS {
		return httpClient
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext:     SafeDialContext,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// ObtainToken authenticates against a remote Cascade server using username and
// password (and optionally a TOTP code), creates an API token, logs out, and
// returns the raw token string.
//
// If the remote has 2FA enabled and totpCode is empty, ErrTOTPRequired is
// returned so the caller can prompt the user. Retrying with the code completes
// the flow.
func ObtainToken(baseURL, username, password, totpCode string, skipTLS bool) (string, error) {
	c := clientFor(skipTLS)
	base := strings.TrimRight(baseURL, "/")

	// ── Step 1: Login ──────────────────────────────────────────────────────
	loginBody, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	loginResp, err := c.Post(base+"/api/session", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		return "", fmt.Errorf("login request: %w", err)
	}
	defer loginResp.Body.Close()

	if loginResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(loginResp.Body)
		return "", fmt.Errorf("login failed (%d): %s", loginResp.StatusCode, strings.TrimSpace(string(body)))
	}

	var loginData map[string]any
	if err := json.NewDecoder(loginResp.Body).Decode(&loginData); err != nil {
		return "", fmt.Errorf("parse login response: %w", err)
	}

	// Extract session cookie — needed for TOTP verify and token creation.
	var sessionCookie string
	for _, ck := range loginResp.Cookies() {
		if ck.Name == "session_id" {
			sessionCookie = ck.Name + "=" + ck.Value
			break
		}
	}
	if sessionCookie == "" {
		return "", fmt.Errorf("no session_id cookie in login response")
	}

	// ── Step 2 (optional): TOTP verification ──────────────────────────────
	if req, _ := loginData["totp_required"].(bool); req {
		if totpCode == "" {
			// Signal the caller to ask for the TOTP code.
			// Log out the pending session to avoid dangling sessions.
			logoutReq, _ := http.NewRequest(http.MethodDelete, base+"/api/session", nil)
			logoutReq.Header.Set("Cookie", sessionCookie)
			if r, e := c.Do(logoutReq); e == nil {
				r.Body.Close()
			}
			return "", ErrTOTPRequired
		}

		verifyBody, _ := json.Marshal(map[string]string{"code": totpCode})
		verifyReq, _ := http.NewRequest(http.MethodPost, base+"/api/auth/totp/verify", bytes.NewReader(verifyBody))
		verifyReq.Header.Set("Content-Type", "application/json")
		verifyReq.Header.Set("Cookie", sessionCookie)

		verifyResp, err := c.Do(verifyReq)
		if err != nil {
			return "", fmt.Errorf("totp verify request: %w", err)
		}
		defer verifyResp.Body.Close()

		if verifyResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(verifyResp.Body)
			return "", fmt.Errorf("totp verify failed (%d): %s", verifyResp.StatusCode, strings.TrimSpace(string(body)))
		}
	} else {
		if auth, _ := loginData["authenticated"].(bool); !auth {
			return "", fmt.Errorf("login did not return authenticated=true")
		}
	}

	// ── Step 3: Create API token ───────────────────────────────────────────
	tokenBody, _ := json.Marshal(map[string]string{
		"name": "cascade-remote",
	})
	tokenReq, _ := http.NewRequest(http.MethodPost, base+"/api/tokens", bytes.NewReader(tokenBody))
	tokenReq.Header.Set("Content-Type", "application/json")
	tokenReq.Header.Set("Cookie", sessionCookie)

	tokenResp, err := c.Do(tokenReq)
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

	// ── Step 4: Logout ─────────────────────────────────────────────────────
	logoutReq, _ := http.NewRequest(http.MethodDelete, base+"/api/session", nil)
	logoutReq.Header.Set("Cookie", sessionCookie)
	if r, e := c.Do(logoutReq); e == nil {
		r.Body.Close()
	}

	return rawToken, nil
}

// Ping checks whether the remote server is reachable and the token is valid.
// Returns nil if healthy.
func Ping(baseURL, token string, skipTLS bool) error {
	base := strings.TrimRight(baseURL, "/")
	req, _ := http.NewRequest(http.MethodGet, base+"/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := clientFor(skipTLS).Do(req)
	if err != nil {
		return fmt.Errorf("ping failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping returned %d", resp.StatusCode)
	}
	return nil
}
