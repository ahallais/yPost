package nzb

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNyuuGeneratorProducesValidXML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.nzb")
	generator, err := NewNyuuGenerator(path, "poster@example.com")
	if err != nil {
		t.Fatalf("NewNyuuGenerator() error = %v", err)
	}

	if err := generator.StartFile("test.bin", []string{"alt.binaries.test"}, time.Unix(1, 0)); err != nil {
		t.Fatalf("StartFile() error = %v", err)
	}
	if err := generator.AddSegment("message-id@example.com", 123); err != nil {
		t.Fatalf("AddSegment() error = %v", err)
	}
	if err := generator.EndFile(); err != nil {
		t.Fatalf("EndFile() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("final NZB exists before Close: %v", err)
	}
	if err := generator.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("os.Open() error = %v", err)
	}
	defer file.Close()

	decoder := xml.NewDecoder(file)
	for {
		if _, err := decoder.Token(); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("generated NZB is invalid XML: %v", err)
		}
	}
}

func TestNyuuGeneratorAbortRemovesTemporaryOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aborted.nzb")
	generator, err := NewNyuuGenerator(path, "poster")
	if err != nil {
		t.Fatal(err)
	}
	if err := generator.StartFile("partial.bin", []string{"alt.test"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := generator.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("aborted final NZB exists: %v", err)
	}
	parts, err := filepath.Glob(filepath.Join(dir, ".aborted.nzb.*.part"))
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 0 {
		t.Fatalf("temporary NZB files remain: %v", parts)
	}
}

func TestNyuuGeneratorEscapesXMLValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "escaped.nzb")
	generator, err := NewNyuuGenerator(path, `Poster & <poster@example>`)
	if err != nil {
		t.Fatal(err)
	}
	if err := generator.StartFile(`a&b<1>.bin`, []string{`alt.binaries.a&b`}, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := generator.AddSegment(`id&value@example`, 1); err != nil {
		t.Fatal(err)
	}
	if err := generator.EndFile(); err != nil {
		t.Fatal(err)
	}
	if err := generator.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := xml.NewDecoder(file)
	for {
		if _, err := decoder.Token(); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("generated NZB is invalid XML: %v", err)
		}
	}
}
