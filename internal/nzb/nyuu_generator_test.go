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
