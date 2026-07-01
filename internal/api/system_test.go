// Package api — unit tests for the system backup/restore helpers added in the
// development branch.
//
// Covered functions:
//   - extractDBFromTarGz    — finds cascade.db inside a tar.gz and returns bytes
//   - currentPhysicalIfaces — calls net.Interfaces() and filters WG/loopback
//   - applyIfaceRemap       — runs SQL UPDATE on nat_rules in the restored DB
//   - createAutoBackup      — writes a tar.gz of the data dir to a dest path
//   - systemListBackups     — GET /api/system/backups lists pre-restore-*.tar.gz files
package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"database/sql"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/JohnnyVBut/cascade/internal/db"
	_ "modernc.org/sqlite"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// makeTarGz builds an in-memory tar.gz containing a single file at archiveName
// with the given content.
func makeTarGz(t *testing.T, archiveName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: archiveName,
		Size: int64(len(content)),
		Mode: 0644,
	}); err != nil {
		t.Fatalf("tar WriteHeader: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("tar Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}
	return buf.Bytes()
}

// ── extractDBFromTarGz ────────────────────────────────────────────────────────

func TestExtractDBFromTarGz_FindsCascadeDB(t *testing.T) {
	dbContent := []byte("fake sqlite bytes")
	tarGz := makeTarGz(t, "cascade.db", dbContent)

	got, err := extractDBFromTarGz(tarGz)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, dbContent) {
		t.Errorf("got %q, want %q", got, dbContent)
	}
}

func TestExtractDBFromTarGz_FindsLegacyAWGDB(t *testing.T) {
	dbContent := []byte("awg db bytes")
	tarGz := makeTarGz(t, "awg.db", dbContent)

	got, err := extractDBFromTarGz(tarGz)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, dbContent) {
		t.Errorf("got %q, want %q", got, dbContent)
	}
}

func TestExtractDBFromTarGz_ErrorWhenNotFound(t *testing.T) {
	tarGz := makeTarGz(t, "other_file.txt", []byte("not a db"))

	_, err := extractDBFromTarGz(tarGz)
	if err == nil {
		t.Fatal("expected error when no DB in archive, got nil")
	}
}

func TestExtractDBFromTarGz_InvalidGzip(t *testing.T) {
	_, err := extractDBFromTarGz([]byte("not gzip data"))
	if err == nil {
		t.Fatal("expected error for invalid gzip, got nil")
	}
}

func TestExtractDBFromTarGz_NestedPathCascadeDB(t *testing.T) {
	// Archive entry with a directory prefix — filepath.Base() should still match.
	dbContent := []byte("nested db bytes")
	tarGz := makeTarGz(t, "data/cascade.db", dbContent)

	got, err := extractDBFromTarGz(tarGz)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, dbContent) {
		t.Errorf("got %q, want %q", got, dbContent)
	}
}

// ── currentPhysicalIfaces ─────────────────────────────────────────────────────

func TestCurrentPhysicalIfaces_FiltersLoopbackAndWG(t *testing.T) {
	// currentPhysicalIfaces calls net.Interfaces() — always available in tests.
	// Verify that no WG/loopback/docker names slip through.
	ifaces := currentPhysicalIfaces()
	for _, name := range ifaces {
		if name == "lo" {
			t.Errorf("loopback interface %q should be filtered out", name)
		}
		if strings.HasPrefix(name, "wg") || strings.HasPrefix(name, "awg") {
			t.Errorf("WireGuard interface %q should be filtered out", name)
		}
		if strings.HasPrefix(name, "docker") {
			t.Errorf("docker interface %q should be filtered out", name)
		}
	}
}

// ── applyIfaceRemap ───────────────────────────────────────────────────────────

func TestApplyIfaceRemap_UpdatesNatRules(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-system-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Build a minimal DB with nat_rules.
	dbPath := filepath.Join(dir, "cascade.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err = sqlDB.Exec(`CREATE TABLE nat_rules (id TEXT PRIMARY KEY, out_interface TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if _, err = sqlDB.Exec(`INSERT INTO nat_rules (id, out_interface) VALUES ('r1','eth0')`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	sqlDB.Close()

	systemDataDir = dir

	if err = applyIfaceRemap(map[string]string{"eth0": "ens3"}); err != nil {
		t.Fatalf("applyIfaceRemap: %v", err)
	}

	sqlDB2, _ := sql.Open("sqlite", dbPath)
	defer sqlDB2.Close()
	var got string
	_ = sqlDB2.QueryRow(`SELECT out_interface FROM nat_rules WHERE id='r1'`).Scan(&got)
	if got != "ens3" {
		t.Errorf("out_interface = %q, want %q", got, "ens3")
	}
}

func TestApplyIfaceRemap_NoDBReturnsError(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-system-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	systemDataDir = dir // no cascade.db present

	err = applyIfaceRemap(map[string]string{"eth0": "ens3"})
	if err == nil {
		t.Fatal("expected error when no DB file exists, got nil")
	}
}

func TestApplyIfaceRemap_EmptyMapIsNoop(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-system-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "cascade.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err = sqlDB.Exec(`CREATE TABLE nat_rules (id TEXT PRIMARY KEY, out_interface TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	sqlDB.Close()

	systemDataDir = dir

	// Empty map — no iterations, but should succeed.
	if err = applyIfaceRemap(map[string]string{}); err != nil {
		t.Fatalf("applyIfaceRemap with empty map: %v", err)
	}
}

// ── createAutoBackup ──────────────────────────────────────────────────────────

func TestCreateAutoBackup_WritesValidTarGz(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-system-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}

	systemDataDir = dir
	destPath := filepath.Join(dir, "pre-restore-test.tar.gz")

	if err := createAutoBackup(destPath); err != nil {
		t.Fatalf("createAutoBackup: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile backup: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("backup file is empty")
	}

	// Verify it is a valid gzip stream containing at least one tar entry.
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip.NewReader on backup: %v", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar.Next: %v", err)
	}
	if hdr.Name == "" {
		t.Error("expected non-empty tar entry name")
	}
}

func TestCreateAutoBackup_EmptyDirWritesNonEmptyArchive(t *testing.T) {
	// When no cascade.db exists, addFileToTar fails silently; the result is
	// still a valid (empty) tar.gz archive.
	dir, err := os.MkdirTemp("", "cascade-system-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}

	systemDataDir = dir
	destPath := filepath.Join(dir, "pre-restore-empty.tar.gz")

	if err := createAutoBackup(destPath); err != nil {
		t.Fatalf("createAutoBackup on empty dir: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// gzip stream must be valid even if the tar inside has no entries.
	if _, err := gzip.NewReader(bytes.NewReader(data)); err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
}

// ── systemListBackups (HTTP handler) ──────────────────────────────────────────

func TestSystemListBackups_EmptyDir(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-system-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(db.Close)

	systemDataDir = dir

	app := fiber.New()
	app.Get("/api/system/backups", systemListBackups)

	req := httptest.NewRequest("GET", "/api/system/backups", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestSystemListBackups_ListsPreRestoreFiles(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-system-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	if err := db.Init(dir); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	t.Cleanup(db.Close)

	// Create one pre-restore file and one irrelevant file.
	if err := os.WriteFile(filepath.Join(dir, "pre-restore-20240101-120000.tar.gz"), []byte("x"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cascade.db"), []byte("db"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	systemDataDir = dir

	app := fiber.New()
	app.Get("/api/system/backups", systemListBackups)

	req := httptest.NewRequest("GET", "/api/system/backups", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	body := buf.String()

	if !strings.Contains(body, "pre-restore-20240101-120000.tar.gz") {
		t.Errorf("response missing pre-restore filename; body=%s", body)
	}
	if strings.Contains(body, "cascade.db") {
		t.Errorf("response must not include non-pre-restore files; body=%s", body)
	}
}
