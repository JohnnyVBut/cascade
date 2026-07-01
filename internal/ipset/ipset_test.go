package ipset

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDestroyAll_EmptyDir verifies that DestroyAll does not panic or return an
// error when the data directory contains no .save files.
func TestDestroyAll_EmptyDir(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-ipset-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	m := &Manager{dataDir: dir}
	// Should not panic; no .save files to iterate.
	m.DestroyAll()
}

// TestDestroyAll_RemovesSaveFiles verifies that DestroyAll removes .save files
// from the data directory. The ipset destroy command will fail (not running as
// root with ipset available), but the file removal is still attempted.
func TestDestroyAll_RemovesSaveFiles(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-ipset-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	// Create a fake .save file.
	saveFile := filepath.Join(dir, "myset.save")
	if err := os.WriteFile(saveFile, []byte("dummy"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	m := &Manager{dataDir: dir}
	m.DestroyAll()

	// The .save file should have been removed.
	if _, err := os.Stat(saveFile); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed after DestroyAll, but it still exists", saveFile)
	}
}

// TestDestroyAll_NonSaveFilesUntouched verifies that files without the .save
// extension are not touched by DestroyAll.
func TestDestroyAll_NonSaveFilesUntouched(t *testing.T) {
	dir, err := os.MkdirTemp("", "cascade-ipset-test-*")
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	defer os.RemoveAll(dir)

	otherFile := filepath.Join(dir, "cascade.db")
	if err := os.WriteFile(otherFile, []byte("db"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	m := &Manager{dataDir: dir}
	m.DestroyAll()

	if _, err := os.Stat(otherFile); err != nil {
		t.Errorf("non-.save file %s was unexpectedly removed: %v", otherFile, err)
	}
}
