package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ypost/internal/nzb"
	"ypost/internal/par2"
	"ypost/internal/sfv"
	"ypost/internal/splitter"
	"ypost/internal/yenc"
	"ypost/pkg/models"
)

func testCompleteWorkflow() {
	fmt.Println("Testing complete NZB posting workflow...")

	// Create a test file
	testFile := "test_file.txt"
	testContent := "This is a test file for the complete NZB posting workflow.\n" +
		"It contains enough content to be split into multiple parts.\n" +
		"We will test yEnc encoding, file splitting, PAR2 generation, SFV creation,\n" +
		"and NZB generation with all components included.\n" +
		"This ensures the complete workflow functions correctly.\n"

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		fmt.Printf("Failed to create test file: %v\n", err)
		return
	}
	defer os.Remove(testFile)

	// Setup directories
	outputDir := "./test_output"
	os.MkdirAll(outputDir, 0755)
	defer os.RemoveAll(outputDir)

	// Initialize components
	split := splitter.NewSplitter(1024, 128) // Small parts for testing
	yencEnc := yenc.Encoder{}
	nzbGen := nzb.NewGenerator(outputDir)
	par2Gen := par2.NewGenerator(outputDir)
	sfvGen := sfv.NewGenerator(outputDir)

	// Test file splitting
	fmt.Println("1. Testing file splitting...")
	parts, err := split.SplitFile(testFile)
	if err != nil {
		fmt.Printf("Failed to split file: %v\n", err)
		return
	}
	fmt.Printf("   File split into %d parts\n", len(parts))

	// Test PAR2 generation
	fmt.Println("2. Testing PAR2 generation...")
	par2Files, err := par2Gen.CreatePAR2(testFile, 15) // 15% redundancy
	if err != nil {
		fmt.Printf("Failed to create PAR2 files: %v\n", err)
		return
	}
	fmt.Printf("   Created %d PAR2 files: %v\n", len(par2Files), par2Files)

	// Test SFV creation
	fmt.Println("3. Testing SFV creation...")
	sfvPath, err := sfvGen.CreateSFV([]string{testFile}, "test_file.sfv")
	if err != nil {
		fmt.Printf("Failed to create SFV file: %v\n", err)
		return
	}
	fmt.Printf("   Created SFV file: %s\n", sfvPath)

	// Simulate posting segments
	fmt.Println("4. Simulating posting segments...")
	var segments []*models.PostSegment
	for i, part := range parts {
		encoded := yencEnc.Encode(part.Data, part.FileName, part.PartNumber, len(parts))
		segment := &models.PostSegment{
			MessageID:   fmt.Sprintf("<test-%d@example.com>", i),
			PartNumber:  part.PartNumber,
			TotalParts:  len(parts),
			FileName:    part.FileName,
			Subject:     fmt.Sprintf("Test file part %d/%d", part.PartNumber, len(parts)),
			PostedAt:    time.Now(),
			BytesPosted: int64(len(encoded)),
		}
		segments = append(segments, segment)
	}

	// Simulate posting PAR2 files
	var par2Segments []*models.PostSegment
	for _, par2File := range par2Files {
		par2Parts, err := split.SplitFile(par2File)
		if err != nil {
			fmt.Printf("Failed to split PAR2 file: %v\n", err)
			continue
		}
		for i, part := range par2Parts {
			encoded := yencEnc.Encode(part.Data, part.FileName, part.PartNumber, len(par2Parts))
			segment := &models.PostSegment{
				MessageID:   fmt.Sprintf("<par2-%d-%d@example.com>", i, part.PartNumber),
				PartNumber:  part.PartNumber,
				TotalParts:  len(par2Parts),
				FileName:    part.FileName,
				Subject:     fmt.Sprintf("PAR2 file part %d/%d", part.PartNumber, len(par2Parts)),
				PostedAt:    time.Now(),
				BytesPosted: int64(len(encoded)),
			}
			par2Segments = append(par2Segments, segment)
		}
	}

	// Simulate posting SFV file
	var sfvSegments []*models.PostSegment
	sfvParts, err := split.SplitFile(sfvPath)
	if err == nil {
		for i, part := range sfvParts {
			encoded := yencEnc.Encode(part.Data, part.FileName, part.PartNumber, len(sfvParts))
			segment := &models.PostSegment{
				MessageID:   fmt.Sprintf("<sfv-%d-%d@example.com>", i, part.PartNumber),
				PartNumber:  part.PartNumber,
				TotalParts:  len(sfvParts),
				FileName:    part.FileName,
				Subject:     fmt.Sprintf("SFV file part %d/%d", part.PartNumber, len(sfvParts)),
				PostedAt:    time.Now(),
				BytesPosted: int64(len(encoded)),
			}
			sfvSegments = append(sfvSegments, segment)
		}
	}

	// Test NZB generation with all components
	fmt.Println("5. Testing NZB generation with all components...")
	additionalFiles := make(map[string][]*models.PostSegment)
	if len(par2Segments) > 0 {
		additionalFiles["PAR2"] = par2Segments
	}
	if len(sfvSegments) > 0 {
		additionalFiles["SFV"] = sfvSegments
	}

	nzbPath, err := nzbGen.Generate("test_file", segments, "alt.binaries.test", additionalFiles)
	if err != nil {
		fmt.Printf("Failed to generate NZB file: %v\n", err)
		return
	}
	fmt.Printf("   Created NZB file: %s\n", nzbPath)

	// Verify files are in the correct location
	fmt.Println("6. Verifying file organization...")
	files, err := filepath.Glob(filepath.Join(outputDir, "*"))
	if err != nil {
		fmt.Printf("Failed to list output files: %v\n", err)
		return
	}
	fmt.Println("   Files in output directory:")
	for _, file := range files {
		fmt.Printf("   - %s\n", filepath.Base(file))
	}

	fmt.Println("\n✅ Complete workflow test successful!")
}

func main() {
	testCompleteWorkflow()
}