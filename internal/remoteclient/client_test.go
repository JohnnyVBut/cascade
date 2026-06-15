// Package remoteclient — tests for the remote-server HTTP client.
//
// These exercise the real network logic of Ping and ObtainToken against
// httptest servers (which bind to 127.0.0.1). The production httpClient installs
// SafeDialContext, which blocks loopback — so TestMain swaps in a vanilla client
// for the duration of these flow tests. The dialer's SSRF behaviour itself is
// covered separately in ssrf_test.go (which calls SafeDialContext directly and
// is unaffected by the swap).
package remoteclient

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain replaces the SSRF-guarded httpClient with a plain client so the
// httptest-based flow tests below can reach 127.0.0.1. Production code is
// unaffected — this runs only in the package test binary.
func TestMain(m *testing.M) {
	orig := httpClient
	httpClient = &http.Client{Timeout: 15 * time.Second}
	code := m.Run()
	httpClient = orig
	os.Exit(code)
}

// ── Ping ────────────────────────────────────────────────────────────────────

func TestPing_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Errorf("Ping hit unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ws_test" {
			t.Errorf("Authorization = %q, want 'Bearer ws_test'", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := Ping(srv.URL, "ws_test", false); err != nil {
		t.Errorf("Ping success: unexpected error %v", err)
	}
}

func TestPing_Non200_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if err := Ping(srv.URL, "ws_bad", false); err == nil {
		t.Error("Ping with 401 response: expected error, got nil")
	}
}

func TestPing_Unreachable_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // close immediately so the port is no longer listening

	if err := Ping(url, "ws_test", false); err == nil {
		t.Error("Ping to closed server: expected error, got nil")
	}
}

// ── ObtainToken ──────────────────────────────────────────────────────────────

// loginServer builds an httptest server that emulates the remote Cascade auth
// flow. The behaviour is controlled by the supplied options.
type loginOpts struct {
	loginStatus    int    // status for POST /api/session (default 200)
	totpRequired   bool   // login response sets totp_required:true
	authenticated  bool   // login response sets authenticated (login mode w/o TOTP)
	noCookie       bool   // omit the session_id cookie on login
	totpStatus     int    // status for POST /api/auth/totp/verify (default 200)
	tokenStatus    int    // status for POST /api/tokens (default 201)
	rawToken       string // raw_token returned by POST /api/tokens
	sawTOTPVerify  *bool  // set true when /api/auth/totp/verify is called
	sawTokenCreate *bool  // set true when /api/tokens is called
}

func loginServer(t *testing.T, o loginOpts) *httptest.Server {
	t.Helper()
	if o.loginStatus == 0 {
		o.loginStatus = http.StatusOK
	}
	if o.totpStatus == 0 {
		o.totpStatus = http.StatusOK
	}
	if o.tokenStatus == 0 {
		o.tokenStatus = http.StatusCreated
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/session":
			if !o.noCookie {
				http.SetCookie(w, &http.Cookie{Name: "session_id", Value: "sess-abc", Path: "/"})
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(o.loginStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totp_required": o.totpRequired,
				"authenticated": o.authenticated,
			})

		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/totp/verify":
			if o.sawTOTPVerify != nil {
				*o.sawTOTPVerify = true
			}
			w.WriteHeader(o.totpStatus)

		case r.Method == http.MethodPost && r.URL.Path == "/api/tokens":
			if o.sawTokenCreate != nil {
				*o.sawTokenCreate = true
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(o.tokenStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"raw_token": o.rawToken})

		case r.Method == http.MethodDelete && r.URL.Path == "/api/session":
			w.WriteHeader(http.StatusOK)

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestObtainToken_Success(t *testing.T) {
	var sawToken bool
	srv := loginServer(t, loginOpts{
		authenticated:  true,
		rawToken:       "ws_generated",
		sawTokenCreate: &sawToken,
	})
	defer srv.Close()

	tok, err := ObtainToken(srv.URL, "admin", "secret", "", false)
	if err != nil {
		t.Fatalf("ObtainToken success: unexpected error %v", err)
	}
	if tok != "ws_generated" {
		t.Errorf("token = %q, want 'ws_generated'", tok)
	}
	if !sawToken {
		t.Error("expected POST /api/tokens to be called")
	}
}

func TestObtainToken_TOTPRequired_NoCode_ReturnsSentinel(t *testing.T) {
	var sawToken bool
	srv := loginServer(t, loginOpts{
		totpRequired:   true,
		sawTokenCreate: &sawToken,
	})
	defer srv.Close()

	_, err := ObtainToken(srv.URL, "admin", "secret", "", false)
	if !errors.Is(err, ErrTOTPRequired) {
		t.Errorf("expected ErrTOTPRequired, got %v", err)
	}
	if sawToken {
		t.Error("token must NOT be created when TOTP code is missing")
	}
}

func TestObtainToken_TOTPRequired_WithCode_Succeeds(t *testing.T) {
	var sawVerify, sawToken bool
	srv := loginServer(t, loginOpts{
		totpRequired:   true,
		rawToken:       "ws_after_totp",
		sawTOTPVerify:  &sawVerify,
		sawTokenCreate: &sawToken,
	})
	defer srv.Close()

	tok, err := ObtainToken(srv.URL, "admin", "secret", "123456", false)
	if err != nil {
		t.Fatalf("ObtainToken with TOTP code: unexpected error %v", err)
	}
	if tok != "ws_after_totp" {
		t.Errorf("token = %q, want 'ws_after_totp'", tok)
	}
	if !sawVerify {
		t.Error("expected POST /api/auth/totp/verify to be called")
	}
	if !sawToken {
		t.Error("expected POST /api/tokens to be called after TOTP verify")
	}
}

func TestObtainToken_TOTPWrongCode_ReturnsError(t *testing.T) {
	srv := loginServer(t, loginOpts{
		totpRequired: true,
		totpStatus:   http.StatusUnauthorized,
	})
	defer srv.Close()

	_, err := ObtainToken(srv.URL, "admin", "secret", "000000", false)
	if err == nil {
		t.Error("wrong TOTP code: expected error, got nil")
	}
	if errors.Is(err, ErrTOTPRequired) {
		t.Error("wrong TOTP code should be a hard error, not ErrTOTPRequired")
	}
}

func TestObtainToken_LoginFailed_ReturnsError(t *testing.T) {
	srv := loginServer(t, loginOpts{loginStatus: http.StatusUnauthorized})
	defer srv.Close()

	_, err := ObtainToken(srv.URL, "admin", "wrong", "", false)
	if err == nil {
		t.Error("wrong password: expected error, got nil")
	}
}

func TestObtainToken_NoSessionCookie_ReturnsError(t *testing.T) {
	srv := loginServer(t, loginOpts{authenticated: true, noCookie: true})
	defer srv.Close()

	_, err := ObtainToken(srv.URL, "admin", "secret", "", false)
	if err == nil {
		t.Error("missing session cookie: expected error, got nil")
	}
	if msg := err.Error(); !strings.Contains(msg, "session_id") {
		t.Errorf("error should mention session_id, got %q", msg)
	}
}

func TestObtainToken_NotAuthenticated_ReturnsError(t *testing.T) {
	// Login returns 200 but authenticated=false and totp_required=false.
	srv := loginServer(t, loginOpts{authenticated: false})
	defer srv.Close()

	_, err := ObtainToken(srv.URL, "admin", "secret", "", false)
	if err == nil {
		t.Error("authenticated=false: expected error, got nil")
	}
}

func TestObtainToken_EmptyRawToken_ReturnsError(t *testing.T) {
	srv := loginServer(t, loginOpts{authenticated: true, rawToken: ""})
	defer srv.Close()

	_, err := ObtainToken(srv.URL, "admin", "secret", "", false)
	if err == nil {
		t.Error("empty raw_token: expected error, got nil")
	}
}
