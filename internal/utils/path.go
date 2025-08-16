package utils

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
}//
 ParseFileSize parses a file size string (e.g., "50MB", "1.5GB") into bytes
func ParseFileSize(sizeStr string) (int64, error) {
	if sizeStr == "" {
		return 0, fmt.Errorf("empty size string")
	}

	// Remove spaces and convert to uppercase
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))
	
	// Regular expression to match number and unit
	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([KMGT]?B?)$`)
	matches := re.FindStringSubmatch(sizeStr)
	
	if len(matches) != 3 {
		return 0, fmt.Errorf("invalid size format: %s", sizeStr)
	}
	
	// Parse the numeric part
	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value: %s", matches[1])
	}
	
	// Parse the unit
	unit := matches[2]
	if unit == "" || unit == "B" {
		return int64(value), nil
	}
	
	var multiplier int64
	switch unit {
	case "KB", "K":
		multiplier = 1024
	case "MB", "M":
		multiplier = 1024 * 1024
	case "GB", "G":
		multiplier = 1024 * 1024 * 1024
	case "TB", "T":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unsupported unit: %s", unit)
	}
	
	return int64(value * float64(multiplier)), nil
}