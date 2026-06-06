package api

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

var systemDataDir string

// SetSystemDataDir must be called once at startup with the data directory path.
func SetSystemDataDir(dir string) {
	systemDataDir = dir
}

// RegisterSystem registers /api/system/* routes.
func RegisterSystem(api fiber.Router) {
	g := api.Group("/system")
	g.Get("/backup", systemBackup)
	g.Post("/restore", systemRestore)
}

// GET /api/system/backup
// Creates a tar.gz archive of awg.db + ipsets/ and streams it as a download.
func systemBackup(c *fiber.Ctx) error {
	tmpFile, err := os.CreateTemp("", "cascade-backup-*.tar.gz")
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create temp file: "+err.Error())
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	gz := gzip.NewWriter(tmpFile)
	tw := tar.NewWriter(gz)

	// awg.db (or wireguard.db — whichever exists)
	for _, dbName := range []string{"awg.db", "wireguard.db"} {
		dbPath := filepath.Join(systemDataDir, dbName)
		if err := addFileToTar(tw, dbPath, dbName); err == nil {
			break // found and added
		}
	}

	// ipset *.save files — stored directly in systemDataDir (not a subdirectory).
	// ipset.Manager saves them as <dataDir>/<setname>.save and restores on startup.
	entries, _ := os.ReadDir(systemDataDir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".save") {
			continue
		}
		_ = addFileToTar(tw, filepath.Join(systemDataDir, e.Name()), e.Name())
	}

	if err := tw.Close(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "tar close: "+err.Error())
	}
	if err := gz.Close(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "gzip close: "+err.Error())
	}
	if err := tmpFile.Close(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "file close: "+err.Error())
	}

	filename := fmt.Sprintf("cascade-backup-%s.tar.gz", time.Now().Format("20060102-150405"))
	return c.Download(tmpName, filename)
}

// POST /api/system/restore
// Accepts a multipart upload of a tar.gz backup and restores files.
// After restore, exits the process so Docker restarts the container.
func systemRestore(c *fiber.Ctx) error {
	fileHeader, err := c.FormFile("backup")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "provide backup file in 'backup' field")
	}

	src, err := fileHeader.Open()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to open uploaded file")
	}
	defer src.Close()

	gr, err := gzip.NewReader(src)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid gzip format: "+err.Error())
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	restored := 0
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid tar format: "+err.Error())
		}

		// Security: prevent path traversal
		target := filepath.Join(systemDataDir, header.Name)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), filepath.Clean(systemDataDir)+string(os.PathSeparator)) {
			log.Printf("system/restore: skipping unsafe path %q", header.Name)
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				log.Printf("system/restore: mkdir %s: %v", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				log.Printf("system/restore: mkdir parent %s: %v", target, err)
				continue
			}
			f, err := os.Create(target)
			if err != nil {
				log.Printf("system/restore: create %s: %v", target, err)
				continue
			}
			if _, err := io.Copy(f, tr); err != nil {
				log.Printf("system/restore: write %s: %v", target, err)
			}
			f.Close()
			restored++
		}
	}

	log.Printf("system/restore: restored %d files from %s", restored, fileHeader.Filename)

	// Send response, then exit — Docker (restart: always) will restart the container.
	if err := c.JSON(fiber.Map{
		"message":  "Backup restored. Container is restarting…",
		"restored": restored,
	}); err != nil {
		return err
	}

	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()

	return nil
}

// addFileToTar adds a single file to the tar writer with the given archive name.
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

	header := &tar.Header{
		Name:    archiveName,
		Size:    info.Size(),
		Mode:    int64(info.Mode()),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}
