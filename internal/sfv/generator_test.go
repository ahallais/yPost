package sfv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateSFVWithProgressReportsHashedBytes(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.bin")
	second := filepath.Join(dir, "second.bin")
	if err := os.WriteFile(first, []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second-file"), 0644); err != nil {
		t.Fatal(err)
	}

	var completed int64
	_, err := NewGenerator(dir).CreateSFVWithProgress([]string{first, second}, "test.sfv", func(delta int64) {
		completed += delta
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed != int64(len("first")+len("second-file")) {
		t.Fatalf("reported %d bytes", completed)
	}
}
