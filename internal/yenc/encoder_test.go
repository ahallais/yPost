package yenc

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"strings"
	"testing"
)

func TestEncodePartMetadataAndRoundTrip(t *testing.T) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i)
	}

	encoded := new(Encoder).EncodePart(data, "archive.bin", 2, 4, 4096, 1025)
	wantLines := []string{
		"=ybegin part=2 total=4 line=128 size=4096 name=archive.bin",
		"=ypart begin=1025 end=2048",
		fmt.Sprintf("=yend size=1024 part=2 pcrc32=%08x", crc32.ChecksumIEEE(data)),
	}
	for _, want := range wantLines {
		if !strings.Contains(encoded, want+"\r\n") {
			t.Errorf("encoded article does not contain %q", want)
		}
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, data) {
		t.Fatal("decoded multipart data differs from input")
	}
}

func TestEncodedLinesDoNotEndWithEscapeMarker(t *testing.T) {
	// The byte 19 encodes to '=' and is placed at the nominal line boundary.
	data := append(bytes.Repeat([]byte{0}, 127), 19, 1)
	encoded := new(Encoder).Encode(data, "boundary.bin", 1, 1)
	lines := strings.Split(encoded, "\r\n")
	for _, line := range lines[1 : len(lines)-2] {
		if strings.HasSuffix(line, "=") {
			t.Fatalf("encoded line ends in half of an escape sequence: %q", line)
		}
	}
	decoded, err := Decode(encoded)
	if err != nil || !bytes.Equal(decoded, data) {
		t.Fatalf("round trip failed: %v", err)
	}
}

func TestEncodePartToMatchesEncodePart(t *testing.T) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i)
	}
	encoder := NewEncoder(128)
	want := encoder.EncodePart(data, "stream.bin", 3, 9, 12345, 4097)
	var streamed bytes.Buffer
	if err := encoder.EncodePartTo(&streamed, data, "stream.bin", 3, 9, 12345, 4097); err != nil {
		t.Fatal(err)
	}
	if streamed.String() != want {
		t.Fatal("streaming encoder output differs from buffered encoder")
	}
}
