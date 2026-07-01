package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JohnnyVBut/cascade/internal/aliases"
	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/firewall"
	"github.com/JohnnyVBut/cascade/internal/tunnel"
	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/scrypt"
	_ "modernc.org/sqlite"
)

var systemDataDir string

// SetSystemDataDir must be called once at startup with the data directory path.
func SetSystemDataDir(dir string) {
	systemDataDir = dir
}

// RegisterSystem registers /api/system/* routes.
func RegisterSystem(api fiber.Router) {
	g := api.Group("/system")
	g.Post("/backup", systemBackup)
	g.Get("/backups", systemListBackups)
	g.Post("/restore/preview", systemRestorePreview)
	g.Post("/restore", systemRestore)
}

// encryptedFileMagic marks a Cascade-encrypted backup: "CASC".
var encryptedFileMagic = [4]byte{'C', 'A', 'S', 'C'}

// deriveKey produces a 32-byte AES key from password + salt using scrypt.
func deriveKey(password string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
}

// encryptBytes encrypts plaintext with AES-256-GCM using password.
// Output format: magic(4) | version(1) | salt(32) | nonce(12) | ciphertext.
func encryptBytes(plaintext []byte, password string) ([]byte, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("rand salt: %w", err)
	}
	key, err := deriveKey(password, salt)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("rand nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	out := make([]byte, 0, 4+1+32+len(nonce)+len(ciphertext))
	out = append(out, encryptedFileMagic[:]...)
	out = append(out, 0x01) // version
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// decryptBytes decrypts a Cascade-encrypted backup.
// Returns an error with a user-facing message on wrong password.
func decryptBytes(data []byte, password string) ([]byte, error) {
	const headerMin = 4 + 1 + 32 // magic + version + salt
	if len(data) < headerMin {
		return nil, fmt.Errorf("file too short to be a valid encrypted backup")
	}
	if [4]byte(data[:4]) != encryptedFileMagic {
		return nil, fmt.Errorf("not a Cascade encrypted backup")
	}
	if data[4] != 0x01 {
		return nil, fmt.Errorf("unsupported backup version: %d", data[4])
	}
	salt := data[5:37]
	key, err := deriveKey(password, salt)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceEnd := 37 + gcm.NonceSize()
	if len(data) <= nonceEnd {
		return nil, fmt.Errorf("file truncated")
	}
	nonce := data[37:nonceEnd]
	plaintext, err := gcm.Open(nil, nonce, data[nonceEnd:], nil)
	if err != nil {
		return nil, fmt.Errorf("wrong password or corrupted backup file")
	}
	return plaintext, nil
}

// POST /api/system/backup
// Body JSON: { password?: string }
// Empty/absent password → plain .tar.gz
// Non-empty password  → AES-256-GCM encrypted .tar.gz.enc
func systemBackup(c *fiber.Ctx) error {
	var body struct {
		Password       string `json:"password"`
		IncludeMetrics bool   `json:"includeMetrics"`
	}
	_ = c.BodyParser(&body)

	// Flush WAL into the main DB file so the snapshot is consistent.
	// Without this, pages modified since the last checkpoint exist only in the
	// WAL file; copying the raw .db file would produce a malformed backup.
	if _, err := db.DB().Exec(`PRAGMA wal_checkpoint(FULL)`); err != nil {
		log.Printf("system/backup: wal_checkpoint: %v (non-fatal)", err)
	}

	// Build tar.gz into memory.
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)

	// cascade.db is the current name; awg.db/wireguard.db are legacy fallbacks.
	// metrics.db is excluded by default — large, regenerates over time.
	for _, dbName := range []string{"cascade.db", "awg.db", "wireguard.db"} {
		if err := addFileToTar(tw, filepath.Join(systemDataDir, dbName), dbName); err == nil {
			break
		}
	}

	if body.IncludeMetrics {
		_ = addFileToTar(tw, filepath.Join(systemDataDir, "metrics.db"), "metrics.db")
	}

	entries, _ := os.ReadDir(systemDataDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".save") {
			_ = addFileToTar(tw, filepath.Join(systemDataDir, e.Name()), e.Name())
		}
	}

	if err := tw.Close(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "tar close: "+err.Error())
	}
	if err := gz.Close(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "gzip close: "+err.Error())
	}

	timestamp := time.Now().Format("20060102-150405")
	tarData := tarBuf.Bytes()

	if body.Password == "" {
		// Plain tar.gz — write to temp file and serve.
		return serveTempFile(c, tarData, fmt.Sprintf("cascade-backup-%s.tar.gz", timestamp))
	}

	// Encrypt and serve .tar.gz.enc.
	enc, err := encryptBytes(tarData, body.Password)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "encrypt: "+err.Error())
	}
	return serveTempFile(c, enc, fmt.Sprintf("cascade-backup-%s.tar.gz.enc", timestamp))
}

// serveTempFile writes data to a temp file and serves it as a download.
func serveTempFile(c *fiber.Ctx, data []byte, filename string) error {
	tmp, err := os.CreateTemp("", "cascade-dl-*")
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "tmp file: "+err.Error())
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fiber.NewError(fiber.StatusInternalServerError, "write tmp: "+err.Error())
	}
	tmp.Close()
	return c.Download(name, filename)
}

// POST /api/system/restore
// Multipart fields: backup (file), password (string, optional), ifaceMap (JSON string, optional).
// ifaceMap example: {"eth0":"ens3"} — renames out_interface in nat_rules after restore.
// Flow: auto-backup → StopAll → FlushAll → DestroyAll → write files → apply remap → restart.
func systemRestore(c *fiber.Ctx) error {
	fileHeader, err := c.FormFile("backup")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "provide backup file in 'backup' field")
	}
	password := c.FormValue("password", "")
	ifaceMapRaw := c.FormValue("ifaceMap", "")

	var ifaceMap map[string]string
	if ifaceMapRaw != "" {
		if err := json.Unmarshal([]byte(ifaceMapRaw), &ifaceMap); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid ifaceMap JSON: "+err.Error())
		}
	}

	src, err := fileHeader.Open()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "open upload: "+err.Error())
	}
	defer src.Close()

	rawData, err := io.ReadAll(src)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "read upload: "+err.Error())
	}

	tarGzData, err := decryptIfNeeded(rawData, password)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	// Validate the tar.gz before doing anything destructive.
	if _, err := gzip.NewReader(bytes.NewReader(tarGzData)); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid gzip: "+err.Error())
	}

	// Step 1: Auto-backup current state to data/pre-restore-TIMESTAMP.tar.gz.
	autoBackupName := fmt.Sprintf("pre-restore-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))
	autoBackupPath := filepath.Join(systemDataDir, autoBackupName)
	if err := createAutoBackup(autoBackupPath); err != nil {
		log.Printf("system/restore: auto-backup failed (non-fatal): %v", err)
	} else {
		log.Printf("system/restore: auto-backup saved to %s", autoBackupName)
	}

	// Step 2: Stop all WireGuard interfaces so kernel state is clean.
	log.Printf("system/restore: stopping all WireGuard interfaces")
	tunnel.Get().StopAll()

	// Step 3: Flush iptables Cascade chains.
	log.Printf("system/restore: flushing firewall chains")
	firewall.Get().FlushAll()

	// Step 4: Destroy all tracked ipsets and their .save files.
	log.Printf("system/restore: destroying ipsets")
	aliases.Get().IpsetMgr().DestroyAll()

	// Step 5: Write files from backup (skip pre-restore backups to avoid recursion).
	gr, _ := gzip.NewReader(bytes.NewReader(tarGzData))
	defer gr.Close()
	tr := tar.NewReader(gr)
	restored := 0
	sep := string(os.PathSeparator)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("system/restore: tar error: %v", err)
			break
		}
		// Never overwrite pre-restore auto-backups.
		if strings.HasPrefix(filepath.Base(header.Name), "pre-restore-") {
			continue
		}
		target := filepath.Join(systemDataDir, header.Name)
		if !strings.HasPrefix(filepath.Clean(target)+sep, filepath.Clean(systemDataDir)+sep) {
			log.Printf("system/restore: skipping unsafe path %q", header.Name)
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, 0755)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				continue
			}
			f, err := os.Create(target)
			if err != nil {
				log.Printf("system/restore: create %s: %v", target, err)
				continue
			}
			_, _ = io.Copy(f, tr)
			f.Close()
			restored++
		}
	}
	log.Printf("system/restore: restored %d files from %s", restored, fileHeader.Filename)

	// Step 6: Remove WAL/SHM files left by the running process — they are incompatible
	// with the newly written DB file and would cause "malformed" on restart.
	for _, dbName := range []string{"cascade.db", "awg.db", "wireguard.db"} {
		base := filepath.Join(systemDataDir, dbName)
		os.Remove(base + "-wal")
		os.Remove(base + "-shm")
	}
	log.Printf("system/restore: WAL/SHM files removed")

	// Step 7: Apply interface remapping in the restored DB.
	if len(ifaceMap) > 0 {
		if err := applyIfaceRemap(ifaceMap); err != nil {
			log.Printf("system/restore: ifaceMap apply failed (non-fatal): %v", err)
		} else {
			log.Printf("system/restore: ifaceMap applied: %v", ifaceMap)
		}
	}

	if err := c.JSON(fiber.Map{"message": "Backup restored. Container is restarting…", "restored": restored}); err != nil {
		return err
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

// createAutoBackup creates a tar.gz of the current dataDir DB and .save files.
func createAutoBackup(destPath string) error {
	if _, err := db.DB().Exec(`PRAGMA wal_checkpoint(FULL)`); err != nil {
		log.Printf("system/auto-backup: wal_checkpoint: %v (non-fatal)", err)
	}

	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)

	for _, dbName := range []string{"cascade.db", "awg.db", "wireguard.db"} {
		if err := addFileToTar(tw, filepath.Join(systemDataDir, dbName), dbName); err == nil {
			break
		}
	}
	entries, _ := os.ReadDir(systemDataDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".save") {
			_ = addFileToTar(tw, filepath.Join(systemDataDir, e.Name()), e.Name())
		}
	}
	_ = tw.Close()
	_ = gz.Close()

	return os.WriteFile(destPath, tarBuf.Bytes(), 0600)
}

// applyIfaceRemap updates out_interface in the restored nat_rules table.
// Only values present in ifaceMap are updated; uses parameterized queries.
func applyIfaceRemap(ifaceMap map[string]string) error {
	// Find the restored DB.
	var dbPath string
	for _, name := range []string{"cascade.db", "awg.db", "wireguard.db"} {
		p := filepath.Join(systemDataDir, name)
		if _, err := os.Stat(p); err == nil {
			dbPath = p
			break
		}
	}
	if dbPath == "" {
		return fmt.Errorf("no DB found in %s", systemDataDir)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	for oldIface, newIface := range ifaceMap {
		if _, err := db.Exec(`UPDATE nat_rules SET out_interface = ? WHERE out_interface = ?`, newIface, oldIface); err != nil {
			log.Printf("system/restore: remap %s→%s: %v", oldIface, newIface, err)
		}
	}
	return nil
}

// ── Pre-restore backup list ───────────────────────────────────────────────────

// GET /api/system/backups
// Returns list of pre-restore auto-backups saved in dataDir.
func systemListBackups(c *fiber.Ctx) error {
	entries, err := os.ReadDir(systemDataDir)
	if err != nil {
		return c.JSON(fiber.Map{"backups": []any{}})
	}
	type backupInfo struct {
		Name      string `json:"name"`
		Size      int64  `json:"size"`
		CreatedAt string `json:"createdAt"`
	}
	var list []backupInfo
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "pre-restore-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, backupInfo{
			Name:      e.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	if list == nil {
		list = []backupInfo{}
	}
	return c.JSON(fiber.Map{"backups": list})
}

// ── Restore preview ───────────────────────────────────────────────────────────

// POST /api/system/restore/preview
// Multipart: backup (file), password (string, optional).
// Returns physical interface names found in backup NAT rules and current server interfaces.
func systemRestorePreview(c *fiber.Ctx) error {
	fileHeader, err := c.FormFile("backup")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "provide backup file in 'backup' field")
	}
	password := c.FormValue("password", "")

	src, err := fileHeader.Open()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "open upload: "+err.Error())
	}
	defer src.Close()
	rawData, err := io.ReadAll(src)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "read upload: "+err.Error())
	}

	tarGzData, err := decryptIfNeeded(rawData, password)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	// Extract DB from backup to a temp file for querying.
	dbBytes, err := extractDBFromTarGz(tarGzData)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "cannot read backup DB: "+err.Error())
	}

	backupIfaces, err := queryNatOutInterfaces(dbBytes)
	if err != nil {
		log.Printf("system/restore/preview: query nat ifaces: %v", err)
		backupIfaces = []string{}
	}

	serverIfaces := currentPhysicalIfaces()

	needsRemap := false
	for _, bi := range backupIfaces {
		found := false
		for _, si := range serverIfaces {
			if si == bi {
				found = true
				break
			}
		}
		if !found {
			needsRemap = true
			break
		}
	}

	return c.JSON(fiber.Map{
		"backupIfaces": backupIfaces,
		"serverIfaces": serverIfaces,
		"needsRemap":   needsRemap,
	})
}

// decryptIfNeeded decrypts backup bytes if encrypted, otherwise returns as-is.
func decryptIfNeeded(rawData []byte, password string) ([]byte, error) {
	if len(rawData) >= 4 && [4]byte(rawData[:4]) == encryptedFileMagic {
		if password == "" {
			return nil, fmt.Errorf("this backup is encrypted — provide the password")
		}
		return decryptBytes(rawData, password)
	}
	return rawData, nil
}

// extractDBFromTarGz finds cascade.db (or legacy names) inside a tar.gz and returns its bytes.
func extractDBFromTarGz(tarGzData []byte) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(tarGzData))
	if err != nil {
		return nil, fmt.Errorf("invalid gzip: %w", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid tar: %w", err)
		}
		base := filepath.Base(hdr.Name)
		if base == "cascade.db" || base == "awg.db" || base == "wireguard.db" {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("no database file found in backup")
}

// queryNatOutInterfaces opens a SQLite DB from bytes and returns distinct out_interface values.
func queryNatOutInterfaces(dbBytes []byte) ([]string, error) {
	tmp, err := os.CreateTemp("", "cascade-preview-*.db")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(dbBytes); err != nil {
		tmp.Close()
		return nil, err
	}
	tmp.Close()

	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT DISTINCT out_interface FROM nat_rules WHERE out_interface != '' AND out_interface IS NOT NULL`)
	if err != nil {
		// Table may not exist in very old backups.
		return []string{}, nil //nolint:nilerr
	}
	defer rows.Close()

	var ifaces []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil && name != "" {
			ifaces = append(ifaces, name)
		}
	}
	return ifaces, nil
}

// currentPhysicalIfaces returns non-WireGuard, non-loopback interface names on the current server.
func currentPhysicalIfaces() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var names []string
	for _, iface := range ifaces {
		n := iface.Name
		if n == "lo" || strings.HasPrefix(n, "wg") || strings.HasPrefix(n, "awg") || strings.HasPrefix(n, "docker") {
			continue
		}
		names = append(names, n)
	}
	return names
}

// addFileToTar adds a single file to the tar archive under archiveName.
func addFileToTar(tw *tar.Writer, filePath, archiveName string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:    archiveName,
		Size:    info.Size(),
		Mode:    int64(info.Mode()),
		ModTime: info.ModTime(),
	}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}
