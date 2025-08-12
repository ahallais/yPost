package utils

import (
	"fmt"
	"path/filepath"
	"time"
)

// GenerateTimestampedFolderName creates a timestamped folder name in the format YYYY-MM-DD_HH-MM-filename
func GenerateTimestampedFolderName(filename string) string {
	// Get current time in local timezone
	now := time.Now()
	
	// Format: YYYY-MM-DD_HH-MM-filename
	// Using 24-hour clock, zero-padded, underscores between date and time, hyphens elsewhere
	timestamp := now.Format("2006-01-02_15-04")
	
	// Remove file extension from filename
	baseName := filename
	if ext := filepath.Ext(filename); ext != "" {
		baseName = filename[:len(filename)-len(ext)]
	}
	
	return fmt.Sprintf("%s-%s", timestamp, baseName)
}

// GetUnifiedOutputPath returns the full path for the unified output directory
func GetUnifiedOutputPath(outputDir, filename string) string {
	folderName := GenerateTimestampedFolderName(filename)
	return filepath.Join(outputDir, folderName)
}