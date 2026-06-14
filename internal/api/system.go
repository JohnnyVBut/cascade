package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/scrypt"
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
		Password string `json:"password"`
	}
	_ = c.BodyParser(&body)

	// Build tar.gz into memory.
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)

	// cascade.db is the current name; awg.db/wireguard.db are legacy fallbacks.
	// metrics.db is intentionally excluded — large, regenerates over time.
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
// Multipart fields: backup (file), password (string, optional)
func systemRestore(c *fiber.Ctx) error {
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

	// Detect encryption.
	var tarGzData []byte
	encrypted := len(rawData) >= 4 && [4]byte(rawData[:4]) == encryptedFileMagic
	if encrypted {
		if password == "" {
			return fiber.NewError(fiber.StatusBadRequest, "this backup is encrypted — provide the password")
		}
		tarGzData, err = decryptBytes(rawData, password)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	} else {
		tarGzData = rawData
	}

	gr, err := gzip.NewReader(bytes.NewReader(tarGzData))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid gzip: "+err.Error())
	}
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
			return fiber.NewError(fiber.StatusBadRequest, "invalid tar: "+err.Error())
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

	log.Printf("system/restore: restored %d files from %s (encrypted=%v)", restored, fileHeader.Filename, encrypted)

	if err := c.JSON(fiber.Map{"message": "Backup restored. Container is restarting…", "restored": restored}); err != nil {
		return err
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
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
