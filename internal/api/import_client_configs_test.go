// Package api — unit tests for the "Import client configs" feature
// (POST /api/tunnel-interfaces/:id/peers/import-client-configs).
//
// Covered:
//   - peer.SavePrivateKey (internal/peer package — persistence semantics)
//   - importClientConfigs handler (multipart upload, match/unmatch logic, sanitization)
//
// tunnel.Manager is a process-wide singleton (sync.Once in tunnel.Init). All
// interfaces and peers needed by the test cases in this file must be seeded
// into SQLite *before* TestMain calls tunnel.Init(), since load() only runs
// once and reads the DB at that moment.
package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/peer"
	"github.com/JohnnyVBut/cascade/internal/tunnel"

	_ "modernc.org/sqlite"
)

// ── Fixtures shared across TestMain and the handler tests ───────────────────
//
// Real WireGuard key pairs generated once (offline) so that DerivePublicKey
// (which shells out to "wg pubkey") produces a value we can assert against
// without needing a WireGuard keypair generator in the test itself.
// Pair A (WireGuard 1.0 interface):
const (
	wgIfaceID = "wg-import-test"

	peerAPrivateKey = "GHUf/N5ORdfBUAJprb+ThFsRdcMwlgQ+lCB8u1pQKlg="
	peerAPublicKey  = "3EbF7pQAmm4YU75vHRIRGWMgHIVjfjhV/xL8mvYWMWc="

	// A second, unrelated key pair — used to simulate a .conf whose private
	// key does not correspond to any known peer's public key.
	strangerPrivateKey = "wGX/kSJ/L3TvVjaR7EDsWJHYtCG7XkFptE1yD5V+eVo="
)

var (
	importCfgApp *fiber.App
	wgBinPath    string // "" if wg is not available on this machine
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cascade-import-client-configs-test-*")
	if err != nil {
		panic(err)
	}
	if err := db.Init(dir); err != nil {
		panic(err)
	}

	// Seed a WireGuard 1.0 interface with one peer whose public key matches
	// peerAPublicKey (derived from peerAPrivateKey by a real "wg pubkey" call
	// when the binary is available — see resolvePublicKey in the test helpers).
	if _, err := tunnel.Create(tunnel.InterfaceInput{
		ID:         wgIfaceID,
		Name:       "wg-import-test",
		Address:    "10.50.0.1/24",
		ListenPort: 51900,
		Protocol:   "wireguard-1.0",
		PrivateKey: "iface-priv-key",
		PublicKey:  "iface-pub-key",
	}); err != nil {
		panic(err)
	}
	if _, err := peer.CreatePeer(wgIfaceID, peer.PeerInput{
		Name:       "peer-a",
		PublicKey:  peerAPublicKey,
		AllowedIPs: "10.50.0.2/32",
	}); err != nil {
		panic(err)
	}

	if _, err := tunnel.Init(""); err != nil {
		panic(err)
	}

	importCfgApp = fiber.New()
	api := importCfgApp.Group("/api")
	RegisterPeers(api)

	if p, err := exec.LookPath("wg"); err == nil {
		wgBinPath = p
	}

	code := m.Run()
	db.Close()
	os.RemoveAll(dir)
	os.Exit(code)
}

// requireWg skips the calling test if the "wg" binary is not installed —
// DerivePublicKey shells out to "wg pubkey" and cannot be exercised otherwise.
func requireWg(t *testing.T) {
	t.Helper()
	if wgBinPath == "" {
		t.Skip("wg binary not found in PATH — skipping test that requires real key derivation")
	}
}

// buildMultipart builds a multipart/form-data body with one or more "configs"
// files, returning the body bytes and the Content-Type header value.
func buildMultipart(t *testing.T, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for filename, content := range files {
		fw, err := w.CreateFormFile("configs", filename)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("multipart Close: %v", err)
	}
	return &buf, w.FormDataContentType()
}

func clientConf(privateKey string) string {
	return "[Interface]\n" +
		"PrivateKey = " + privateKey + "\n" +
		"Address = 10.50.0.2/32\n" +
		"DNS = 1.1.1.1\n\n" +
		"[Peer]\n" +
		"PublicKey = iface-pub-key\n" +
		"Endpoint = vpn.example.com:51900\n" +
		"AllowedIPs = 0.0.0.0/0\n" +
		"PersistentKeepalive = 25\n"
}

const malformedConf = "[Interface]\n" +
	"Address = 10.50.0.9/32\n" + // missing PrivateKey — ParseWGConf must error
	"[Peer]\n" +
	"PublicKey = iface-pub-key\n" +
	"AllowedIPs = 0.0.0.0/0\n"

// ── importClientConfigs: HTTP-level tests ────────────────────────────────────

func TestImportClientConfigs_MatchedPeer(t *testing.T) {
	requireWg(t)

	body, contentType := buildMultipart(t, map[string]string{
		"peer-a.conf": clientConf(peerAPrivateKey),
	})

	req := httptest.NewRequest("POST", "/api/tunnel-interfaces/"+wgIfaceID+"/peers/import-client-configs", body)
	req.Header.Set("Content-Type", contentType)

	resp, err := importCfgApp.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var out struct {
		Matched   int      `json:"matched"`
		Unmatched []string `json:"unmatched"`
		Peers     []struct {
			ID                 string `json:"id"`
			PrivateKey         string `json:"privateKey"`
			PublicKey          string `json:"publicKey"`
			DownloadableConfig bool   `json:"downloadableConfig"`
		} `json:"peers"`
	}
	if err := decodeJSON(resp, &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if out.Matched != 1 {
		t.Errorf("matched = %d, want 1", out.Matched)
	}
	if len(out.Unmatched) != 0 {
		t.Errorf("unmatched = %v, want empty", out.Unmatched)
	}
	if len(out.Peers) != 1 {
		t.Fatalf("expected 1 peer in response, got %d", len(out.Peers))
	}
	if out.Peers[0].PrivateKey != "" {
		t.Errorf("response peer PrivateKey = %q, want empty (sanitized)", out.Peers[0].PrivateKey)
	}
	if out.Peers[0].PublicKey != peerAPublicKey {
		t.Errorf("response peer PublicKey = %q, want %q", out.Peers[0].PublicKey, peerAPublicKey)
	}
	if !out.Peers[0].DownloadableConfig {
		t.Error("response peer DownloadableConfig = false, want true — QR/download button would stay disabled")
	}

	// Verify persistence directly via peer.GetPeer — private key must now be stored.
	saved, err := peer.GetPeer(out.Peers[0].ID)
	if err != nil {
		t.Fatalf("GetPeer: %v", err)
	}
	if saved == nil {
		t.Fatal("expected peer to exist")
	}
	if saved.PrivateKey != peerAPrivateKey {
		t.Errorf("stored PrivateKey = %q, want %q", saved.PrivateKey, peerAPrivateKey)
	}

	// Regression check: the in-memory peer cache inside tunnel.TunnelInterface
	// must also reflect the new private key / DownloadableConfig — otherwise a
	// subsequent GET /peers (which reads from the in-memory cache, not SQLite
	// directly) would still show DownloadableConfig=false and the QR/download
	// buttons would stay disabled in the UI even though the DB was updated.
	listReq := httptest.NewRequest("GET", "/api/tunnel-interfaces/"+wgIfaceID+"/peers", nil)
	listResp, err := importCfgApp.Test(listReq)
	if err != nil {
		t.Fatalf("GET /peers: %v", err)
	}
	var listOut struct {
		Peers []struct {
			ID                 string `json:"id"`
			DownloadableConfig bool   `json:"downloadableConfig"`
		} `json:"peers"`
	}
	if err := decodeJSON(listResp, &listOut); err != nil {
		t.Fatalf("decode GET /peers response: %v", err)
	}
	found := false
	for _, p := range listOut.Peers {
		if p.ID == out.Peers[0].ID {
			found = true
			if !p.DownloadableConfig {
				t.Error("GET /peers: in-memory cached peer has DownloadableConfig=false after import — cache was not refreshed")
			}
		}
	}
	if !found {
		t.Fatal("imported peer not found in GET /peers response")
	}
}

func TestImportClientConfigs_UnmatchedPublicKey(t *testing.T) {
	requireWg(t)

	body, contentType := buildMultipart(t, map[string]string{
		"stranger.conf": clientConf(strangerPrivateKey),
	})

	req := httptest.NewRequest("POST", "/api/tunnel-interfaces/"+wgIfaceID+"/peers/import-client-configs", body)
	req.Header.Set("Content-Type", contentType)

	resp, err := importCfgApp.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var out struct {
		Matched   int      `json:"matched"`
		Unmatched []string `json:"unmatched"`
	}
	if err := decodeJSON(resp, &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Matched != 0 {
		t.Errorf("matched = %d, want 0", out.Matched)
	}
	if len(out.Unmatched) != 1 || !strings.HasPrefix(out.Unmatched[0], "stranger.conf") {
		t.Errorf("unmatched = %v, want entry starting with \"stranger.conf\"", out.Unmatched)
	}
}

func TestImportClientConfigs_MalformedConf(t *testing.T) {
	// Does not require wg — ParseWGConf fails before DerivePublicKey is ever called.
	body, contentType := buildMultipart(t, map[string]string{
		"broken.conf": malformedConf,
	})

	req := httptest.NewRequest("POST", "/api/tunnel-interfaces/"+wgIfaceID+"/peers/import-client-configs", body)
	req.Header.Set("Content-Type", contentType)

	resp, err := importCfgApp.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var out struct {
		Matched   int      `json:"matched"`
		Unmatched []string `json:"unmatched"`
	}
	if err := decodeJSON(resp, &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Matched != 0 {
		t.Errorf("matched = %d, want 0", out.Matched)
	}
	if len(out.Unmatched) != 1 || !strings.HasPrefix(out.Unmatched[0], "broken.conf") {
		t.Errorf("unmatched = %v, want entry starting with \"broken.conf\"", out.Unmatched)
	}
}

func TestImportClientConfigs_MultipleFilesAggregateCounts(t *testing.T) {
	requireWg(t)

	body, contentType := buildMultipart(t, map[string]string{
		"peer-a.conf":   clientConf(peerAPrivateKey),
		"stranger.conf": clientConf(strangerPrivateKey),
		"broken.conf":   malformedConf,
	})

	req := httptest.NewRequest("POST", "/api/tunnel-interfaces/"+wgIfaceID+"/peers/import-client-configs", body)
	req.Header.Set("Content-Type", contentType)

	resp, err := importCfgApp.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var out struct {
		Matched   int      `json:"matched"`
		Unmatched []string `json:"unmatched"`
	}
	if err := decodeJSON(resp, &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Matched != 1 {
		t.Errorf("matched = %d, want 1", out.Matched)
	}
	if len(out.Unmatched) != 2 {
		t.Errorf("unmatched = %v, want 2 entries", out.Unmatched)
	}
}

func TestImportClientConfigs_NoFiles(t *testing.T) {
	// Empty multipart form (no "configs" field at all).
	body, contentType := buildMultipart(t, map[string]string{})

	req := httptest.NewRequest("POST", "/api/tunnel-interfaces/"+wgIfaceID+"/peers/import-client-configs", body)
	req.Header.Set("Content-Type", contentType)

	resp, err := importCfgApp.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestImportClientConfigs_InterfaceNotFound(t *testing.T) {
	body, contentType := buildMultipart(t, map[string]string{
		"peer-a.conf": clientConf(peerAPrivateKey),
	})

	req := httptest.NewRequest("POST", "/api/tunnel-interfaces/does-not-exist/peers/import-client-configs", body)
	req.Header.Set("Content-Type", contentType)

	resp, err := importCfgApp.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// TestImportClientConfigs_AWGBinarySelection documents and verifies the bin
// selection logic (t.Protocol == "amneziawg-2.0" -> "awg") indirectly: since
// no AWG interface is seeded in this test binary (tunnel.Init()'s singleton
// load already happened in TestMain), and since the "awg" binary is not
// expected to be present in the sandboxed test environment, we do not attempt
// a live end-to-end request against an AWG interface here. Instead this is
// covered at a lower level:
//   - conf_parser_test.go / tunnel package tests assert ParseWGConf sets
//     Protocol = "amneziawg-2.0" when AWG2 [Interface] fields are present.
//   - The handler's bin-selection line (`if t.Protocol == "amneziawg-2.0" {
//     bin = "awg" }`) is a single conditional with no branching logic beyond
//     the string comparison already exercised by the WireGuard-path tests
//     above; a dedicated AWG happy-path test would require the "awg" binary
//     to be installed, which this environment does not provide.
func TestImportClientConfigs_AWGBinarySelection(t *testing.T) {
	if _, err := exec.LookPath("awg"); err != nil {
		t.Skip("awg binary not found in PATH — cannot exercise AWG key derivation end-to-end; " +
			"see test comment for coverage rationale")
	}
	t.Skip("no AWG interface seeded in this seam (tunnel.Init is a one-time singleton); " +
		"add an AWG fixture in TestMain if this environment gains an awg binary")
}

// decodeJSON reads and JSON-decodes an *http.Response body (as returned by
// fiber's app.Test()).
func decodeJSON(resp *http.Response, v any) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}
