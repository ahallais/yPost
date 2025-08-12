package main

import (
	"fmt"
	"time"

	"ypost/internal/progress"
)

func main() {
	// Test the progress tracking system with progress bar
	fmt.Println("Testing progress tracking system with progress bar...")
	
	// Simulate a file with 12 chunks
	filename := "test_file.bin"
	totalChunks := 12
	totalBytes := int64(1024 * 1024) // 1MB
	
	tracker := progress.NewTracker(filename, totalChunks, totalBytes)
	
	// Simulate uploading chunks
	chunkSize := totalBytes / int64(totalChunks)
	
	for i := 1; i <= totalChunks; i++ {
		// Simulate some work
		time.Sleep(100 * time.Millisecond)
		
		// Emit progress - this will update the progress bar
		tracker.EmitProgress(i, chunkSize)
	}
	
	// Mark as complete - this will finish the progress bar
	tracker.EmitComplete()
}