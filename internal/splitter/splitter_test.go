package splitter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetPartFileNameUsesPlainNumericSplits(t *testing.T) {
	s := NewSplitter(10)
	if got := s.GetPartFileName("release.7z", 1, 12); got != "release.7z.001" {
		t.Fatalf("GetPartFileName() = %q", got)
	}
	if got := s.GetPartFileName("release.7z", 1, 1); got != "release.7z" {
		t.Fatalf("single part name = %q", got)
	}
}

func TestSplitFileWithProgressReportsAllBytes(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.bin")
	data := []byte("0123456789abcdef")
	if err := os.WriteFile(input, data, 0644); err != nil {
		t.Fatal(err)
	}

	var completed int64
	parts, err := NewSplitter(5).SplitFileWithProgress(input, filepath.Join(dir, "parts"), func(delta int64) {
		completed += delta
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed != int64(len(data)) {
		t.Fatalf("reported %d bytes, want %d", completed, len(data))
	}
	if len(parts) != 4 {
		t.Fatalf("created %d parts, want 4", len(parts))
	}
}
