package par2

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkPAR2Generation benchmarks the PAR2 generation performance
func BenchmarkPAR2Generation(b *testing.B) {
	// Create a temporary test file
	tempDir := b.TempDir()
	testFile := filepath.Join(tempDir, "test.bin")
	
	// Create test data (1MB file)
	testData := make([]byte, 1024*1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	
	err := os.WriteFile(testFile, testData, 0644)
	if err != nil {
		b.Fatal(err)
	}
	
	generator := NewGenerator("")
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		_, err := generator.generateRecoveryData(testFile, 64*1024, 10)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkXOROptimized benchmarks the optimized XOR function
func BenchmarkXOROptimized(b *testing.B) {
	generator := NewGenerator("")
	
	// Create test data
	dst := make([]byte, 64*1024)
	src := make([]byte, 64*1024)
	for i := range src {
		src[i] = byte(i % 256)
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		generator.xorBytesOptimized(dst, src)
	}
}

// BenchmarkXORBasic benchmarks the basic XOR function
func BenchmarkXORBasic(b *testing.B) {
	generator := NewGenerator("")
	
	// Create test data
	dst := make([]byte, 64*1024)
	src := make([]byte, 64*1024)
	for i := range src {
		src[i] = byte(i % 256)
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		generator.xorBytes(dst, src)
	}
}