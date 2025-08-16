package par2

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPAR2Generation(t *testing.T) {
	// Create temporary directory
	tempDir := t.TempDir()
	
	// Create test file
	testFile := filepath.Join(tempDir, "test.txt")
	testData := []byte("Hello, World! This is a test file for PAR2 generation.")
	
	err := os.WriteFile(testFile, testData, 0644)
	if err != nil {
		t.Fatal(err)
	}
	
	// Create PAR2 generator
	generator := NewGenerator("")
	
	// Generate PAR2 files
	par2Files, err := generator.CreatePAR2(testFile, 10)
	if err != nil {
		t.Fatal(err)
	}
	
	// Verify PAR2 files were created
	if len(par2Files) == 0 {
		t.Fatal("No PAR2 files were created")
	}
	
	// Check if main PAR2 file exists
	if _, err := os.Stat(par2Files[0]); os.IsNotExist(err) {
		t.Fatal("Main PAR2 file was not created")
	}
	
	t.Logf("Successfully created %d PAR2 files", len(par2Files))
}

func TestXORFunctions(t *testing.T) {
	generator := NewGenerator("")
	
	// Test data
	dst := []byte{0x00, 0x11, 0x22, 0x33}
	src := []byte{0xFF, 0xEE, 0xDD, 0xCC}
	expected := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	
	// Test basic XOR
	dstCopy := make([]byte, len(dst))
	copy(dstCopy, dst)
	generator.xorBytes(dstCopy, src)
	
	for i, v := range expected {
		if dstCopy[i] != v {
			t.Errorf("Basic XOR failed at index %d: got %02x, want %02x", i, dstCopy[i], v)
		}
	}
	
	// Test optimized XOR
	dstCopy2 := make([]byte, len(dst))
	copy(dstCopy2, dst)
	generator.xorBytesOptimized(dstCopy2, src)
	
	for i, v := range expected {
		if dstCopy2[i] != v {
			t.Errorf("Optimized XOR failed at index %d: got %02x, want %02x", i, dstCopy2[i], v)
		}
	}
}