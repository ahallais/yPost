package progress

import (
	"bytes"
	"strings"
	"testing"
)

func TestAnimatedBarWriterCyclesAtProgressEdge(t *testing.T) {
	var output bytes.Buffer
	writer := &animatedBarWriter{out: &output}
	line := "Uploading file 50% |█████     |"

	if _, err := writer.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "|█████⠁    |") {
		t.Fatalf("first frame = %q", got)
	}

	output.Reset()
	if _, err := writer.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "|█████⠃    |") {
		t.Fatalf("second frame = %q", got)
	}
}

func TestAnimatedBarWriterLeavesCompletedBarAlone(t *testing.T) {
	var output bytes.Buffer
	writer := &animatedBarWriter{out: &output}
	line := "Uploading file 100% |██████████|"

	if _, err := writer.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != line {
		t.Fatalf("completed frame = %q, want %q", got, line)
	}
}
