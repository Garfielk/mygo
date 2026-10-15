//go:build windows

package mygo

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenSharedLetsAppsSave replaces a document's file, as editors save,
// while the app reads it: os.Open refuses that ("Access is denied"),
// openShared lets it happen, and the app goes on reading what it opened.
func TestOpenSharedLetsAppsSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	save := func() error {
		if err := os.WriteFile(path+".tmp", []byte("two"), 0o644); err != nil {
			t.Fatal(err)
		}
		return os.Rename(path+".tmp", path)
	}

	plain, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if save() == nil {
		t.Log("os.Open let the file be replaced: this system shares deletion anyway")
	}
	plain.Close()

	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := openShared(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := save(); err != nil {
		t.Fatalf("saving over a file the app reads: %v", err)
	}
	if b, err := io.ReadAll(f); err != nil || string(b) != "one" {
		t.Errorf("read %q, %v from the file opened before the save", b, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "two" {
		t.Errorf("the file has %q after the save", b)
	}
}
