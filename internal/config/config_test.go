package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	if cfg.Output.KeepTempFiles {
		t.Fatal("temporary upload files should not be kept by default")
	}
}

func TestLoadConfigKeepTempFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("nntp:\n  server: news.example\nposting:\n  group: alt.binaries.test\noutput:\n  keep_temp_files: true\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Output.KeepTempFiles {
		t.Fatal("output.keep_temp_files was not loaded")
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

func TestLegacyConnectionsMapsToServerMaxConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("nntp:\n  server: news.example\n  connections: 5\nposting:\n  group: alt.binaries.test\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.NNTP.Servers[0].MaxConns; got != 5 {
		t.Fatalf("MaxConns = %d, want 5", got)
	}
	server := cfg.NNTP.Servers[0]
	if server.ConnectTimeout != 30*time.Second || server.CommandTimeout != 30*time.Second || server.PostTimeout != 120*time.Second {
		t.Fatalf("unexpected timeout defaults: connect=%v command=%v post=%v",
			server.ConnectTimeout, server.CommandTimeout, server.PostTimeout)
	}
	if server.ReconnectDelay != 15*time.Second || server.RequestRetries != 5 || server.PostRetries != 1 {
		t.Fatalf("unexpected retry defaults: delay=%v request=%d post=%d",
			server.ReconnectDelay, server.RequestRetries, server.PostRetries)
	}
}
