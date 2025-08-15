package main

import (
	"fmt"
	"os"
	"time"

	"ypost/internal/nzb"
	"ypost/pkg/models"
)

func testNZBGeneration() {
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "nzb-test")
	if err != nil {
		fmt.Printf("Failed to create temp dir: %v\n", err)
		return
	}
	defer os.RemoveAll(tempDir)

	// Create test segments with realistic message IDs
	segments := []*models.PostSegment{
		{
			MessageID:   "abcd1234-5678901234567@nyuu",
			PartNumber:  1,
			TotalParts:  3,
			FileName:    "test-file.txt",
			Subject:     "test-file.txt yEnc (1/3)",
			PostedAt:    time.Now(),
			BytesPosted: 750000,
		},
		{
			MessageID:   "efgh5678-5678901234568@nyuu",
			PartNumber:  2,
			TotalParts:  3,
			FileName:    "test-file.txt",
			Subject:     "test-file.txt yEnc (2/3)",
			PostedAt:    time.Now(),
			BytesPosted: 750000,
		},
		{
			MessageID:   "ijkl9012-5678901234569@nyuu",
			PartNumber:  3,
			TotalParts:  3,
			FileName:    "test-file.txt",
			Subject:     "test-file.txt yEnc (3/3)",
			PostedAt:    time.Now(),
			BytesPosted: 500000,
		},
	}

	// Generate NZB
	generator := nzb.NewGenerator(tempDir, "test-poster@example.com")
	nzbPath, err := generator.Generate("test-file.txt", segments, "alt.binaries.test", nil)
	if err != nil {
		fmt.Printf("Failed to generate NZB: %v\n", err)
		return
	}

	// Read and display the generated NZB
	content, err := os.ReadFile(nzbPath)
	if err != nil {
		fmt.Printf("Failed to read NZB file: %v\n", err)
		return
	}

	fmt.Println("Generated NZB content:")
	fmt.Println(string(content))
	fmt.Printf("\nNZB file generated successfully: %s\n", nzbPath)
}

func main() {
	testNZBGeneration()
}