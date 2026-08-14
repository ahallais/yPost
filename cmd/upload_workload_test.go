package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkloadAwareConnectionLimit(t *testing.T) {
	dir := t.TempDir()
	writeSizedFile := func(name string, size int64) string {
		t.Helper()
		path := filepath.Join(dir, name)
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(size); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		return path
	}

	for _, test := range []struct {
		name       string
		sizes      []int64
		requested  int
		article    int64
		target     int64
		want       int
		wantBytes  int64
		wantPieces int64
	}{
		{name: "small upload uses one", sizes: []int64{500}, requested: 20, article: 100, target: 800, want: 1, wantBytes: 500, wantPieces: 5},
		{name: "byte target grows worker count", sizes: []int64{2500}, requested: 20, article: 100, target: 800, want: 4, wantBytes: 2500, wantPieces: 25},
		{name: "configured maximum is a ceiling", sizes: []int64{5000}, requested: 3, article: 100, target: 800, want: 3, wantBytes: 5000, wantPieces: 50},
		{name: "single file article count is useful cap", sizes: []int64{100, 100, 100}, requested: 20, article: 100, target: 1, want: 1, wantBytes: 300, wantPieces: 3},
		{name: "default target is eight articles", sizes: []int64{1700}, requested: 20, article: 100, target: 0, want: 3, wantBytes: 1700, wantPieces: 17},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := make([]string, len(test.sizes))
			for i, size := range test.sizes {
				paths[i] = writeSizedFile(test.name+string(rune('a'+i)), size)
			}
			got, workload, err := workloadAwareConnectionLimit(test.requested, test.article, test.target, paths)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want || workload.Bytes != test.wantBytes || workload.Articles != test.wantPieces {
				t.Fatalf("limit/workload = %d/%+v, want %d/{Bytes:%d Articles:%d}",
					got, workload, test.want, test.wantBytes, test.wantPieces)
			}
		})
	}
}

func TestWorkloadAwareConnectionLimitReportsMissingFile(t *testing.T) {
	if _, _, err := workloadAwareConnectionLimit(4, 100, 800, []string{"missing"}); err == nil {
		t.Fatal("expected missing upload file error")
	}
}
