package frontend

import (
	"io"
	"testing"
)

// TestUI2FS_ServesIndex verifies the embedded UI2 filesystem exposes index.html
// at its root (either the tracked placeholder or a real Vite build — both put
// index.html at the root).
func TestUI2FS_ServesIndex(t *testing.T) {
	fsys := UI2FS()
	f, err := fsys.Open("index.html")
	if err != nil {
		t.Fatalf("open index.html: %v", err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("index.html is empty")
	}
}

// TestUI2FS_MissingFileErrors confirms unknown paths return an error (the HTTP
// layer turns this into the NotFoundFile SPA fallback, not a panic).
func TestUI2FS_MissingFileErrors(t *testing.T) {
	fsys := UI2FS()
	if _, err := fsys.Open("does-not-exist.js"); err == nil {
		t.Fatal("expected error opening missing file, got nil")
	}
}
