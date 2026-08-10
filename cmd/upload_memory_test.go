package cmd

import "testing"

func TestParseAvailableMemory(t *testing.T) {
	got, err := parseAvailableMemory("MemTotal: 524288 kB\nMemAvailable: 262144 kB\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(256 * 1024 * 1024); got != want {
		t.Fatalf("available memory = %d, want %d", got, want)
	}
}

func TestMemoryAwareConnectionLimit(t *testing.T) {
	const available = int64(256 * 1024 * 1024)
	if got, _ := memoryAwareConnectionLimit(5, 500_000, available); got != 5 {
		t.Fatalf("five configured connections reduced to %d", got)
	}
	if got, _ := memoryAwareConnectionLimit(50, 500_000, available); got >= 50 || got < 2 {
		t.Fatalf("50 configured connections limited to %d, want a conservative multi-worker limit", got)
	}
	if got, _ := memoryAwareConnectionLimit(5, 500_000, 8*1024*1024); got != 1 {
		t.Fatalf("low-memory connection limit = %d, want 1", got)
	}
}
