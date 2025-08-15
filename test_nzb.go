package main

import (
	"fmt"
	"os"
	"time"

	"ypost/internal/nzb"
	"ypost/pkg/models"
)

func testNZBGeneration() {
	// Create test segments
	segments := []*models.PostSegment{
		{
			MessageID:   "part1of3.test@example.com",
			PartNumber:  1,
			TotalParts:  3,
			FileName:    "test-file.txt",
			Subject:     "Test file posting",
			PostedAt:    time.Now(),
			BytesPosted: 250000,
		},
		{
			MessageID:   "part2of3.test@example.com",
			PartNumber:  2,
			TotalParts:  3,
			FileName:    "test-file.txt",
			Subject:     "Test file posting",
			PostedAt:    time.Now(),
			BytesPosted: 250000,
		},
		{
			MessageID:   "part3of3.test@example.com",
			PartNumber:  3,
			TotalParts:  3,
			FileName:    "test-file.txt",
			Subject:     "Test file posting",
			PostedAt:    time.Now(),
			BytesPosted: 180000,
		},
	}

	// Create NZB generator with test poster
	testPoster := "test@example.com"
	generator := nzb.NewGenerator("./test_output", testPoster)
	
	// Generate NZB with multiple groups
	nzbPath, err := generator.Generate("test-file.txt", segments, "alt.binaries.test,alt.binaries.misc", nil)
	if err != nil {
		fmt.Printf("Error generating NZB: %v\n", err)
		return
	}

	fmt.Printf("NZB generated successfully: %s\n", nzbPath)
	
	// Read and display the generated NZB
	content, err := os.ReadFile(nzbPath)
	if err != nil {
		fmt.Printf("Error reading NZB file: %v\n", err)
		return
	}
	
	fmt.Println("\nGenerated NZB content:")
	fmt.Println(string(content))
}

func main() {
	testNZBGeneration()
}