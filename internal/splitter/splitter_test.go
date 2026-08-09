package splitter

import "testing"

func TestGetPartFileNameUsesPlainNumericSplits(t *testing.T) {
	s := NewSplitter(10)
	if got := s.GetPartFileName("release.7z", 1, 12); got != "release.7z.001" {
		t.Fatalf("GetPartFileName() = %q", got)
	}
	if got := s.GetPartFileName("release.7z", 1, 1); got != "release.7z" {
		t.Fatalf("single part name = %q", got)
	}
}
