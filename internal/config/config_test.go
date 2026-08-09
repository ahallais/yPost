package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.yaml")
	// An explicit missing file is an error; use a directory with no config and
	// temporarily work there to exercise built-in defaults.
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	_ = path
	cfg, _, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Posting.MaxPartSize != 768000 || cfg.Posting.MaxArticleSize != 768000 {
		t.Fatalf("unexpected posting sizes: part=%d article=%d", cfg.Posting.MaxPartSize, cfg.Posting.MaxArticleSize)
	}
}

func TestLegacySplittingSizeOverridesPostingSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("nntp:\n  server: news.example\nposting:\n  group: alt.binaries.test\n  max_part_size: 1234\nsplitting:\n  max_file_size: 2MB\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Posting.MaxPartSize != 2*1024*1024 {
		t.Fatalf("MaxPartSize = %d", cfg.Posting.MaxPartSize)
	}
}
