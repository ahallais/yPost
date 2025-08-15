package nzb

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ypost/pkg/models"
)

// Generator handles NZB file generation
type Generator struct {
	outputDir string
	poster    string
}

// NewGenerator creates a new NZB generator
func NewGenerator(outputDir string, poster string) *Generator {
	return &Generator{
		outputDir: outputDir,
		poster:    poster,
	}
}

// Generate creates an NZB file from posting results
func (g *Generator) Generate(fileName string, segments []*models.PostSegment, group string, additionalFiles map[string][]*models.PostSegment) (string, error) {
	if err := os.MkdirAll(g.outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	nzbContent := g.buildNZBContent(fileName, segments, group, additionalFiles)
	
	filePath := filepath.Join(g.outputDir, fmt.Sprintf("%s.nzb", sanitizeFileName(fileName)))
	
	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create NZB file: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(nzbContent)
	if err != nil {
		return "", fmt.Errorf("failed to write NZB file: %w", err)
	}

	return filePath, nil
}

// buildNZBContent constructs the NZB XML content as a string
func (g *Generator) buildNZBContent(fileName string, segments []*models.PostSegment, group string, additionalFiles map[string][]*models.PostSegment) string {
	var content strings.Builder
	
	// Add XML declaration and DOCTYPE - updated to NZB 1.1
	content.WriteString(`<?xml version="1.0" encoding="iso-8859-1"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head>
    <meta type="title">` + sanitizeXML(fileName) + `</meta>
    <meta type="category">misc</meta>
    <meta type="tag">AI</meta>
  </head>
`)
	
	// Process all files (main file + additional files)
	allFiles := []struct {
		name     string
		segments []*models.PostSegment
	}{
		{fileName, segments},
	}
	
	// Add additional files
	for name, fileSegments := range additionalFiles {
		if len(fileSegments) > 0 {
			allFiles = append(allFiles, struct {
				name     string
				segments []*models.PostSegment
			}{name, fileSegments})
		}
	}
	
	// Split group string by comma for multiple groups
	groups := strings.Split(group, ",")
	for i := range groups {
		groups[i] = strings.TrimSpace(groups[i])
	}
	
	// Create file entries
	for _, file := range allFiles {
		if len(file.segments) == 0 {
			continue
		}
		
		// Use the configured poster value
		poster := g.poster
		date := time.Now().Unix()
		
		// Use the actual subject from the segment
		subject := file.segments[0].Subject
		
		content.WriteString(fmt.Sprintf(`  <file poster="%s" date="%d" subject="%s">
    <groups>
`, sanitizeXML(poster), date, sanitizeXML(subject)))
		
		// Add all groups
		for _, g := range groups {
			content.WriteString(fmt.Sprintf(`      <group>%s</group>
`, sanitizeXML(g)))
		}
		
		content.WriteString(`    </groups>
    <segments>
`)
		
		// Add segments with actual message IDs
		for _, segment := range file.segments {
			segmentID := g.generateSegmentID(segment.MessageID)
			content.WriteString(fmt.Sprintf(`      <segment bytes="%d" number="%d">%s</segment>
`, segment.BytesPosted, segment.PartNumber, segmentID))
		}
		
		content.WriteString(`    </segments>
  </file>
`)
	}
	
	content.WriteString("</nzb>")
	return content.String()
}

// generateUniqueID creates a unique identifier for a file
func (g *Generator) generateUniqueID() string {
	const safeChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	const length = 16
	
	var result strings.Builder
	for i := 0; i < length; i++ {
		result.WriteByte(safeChars[time.Now().UnixNano()%int64(len(safeChars))])
	}
	return result.String()
}

// generateSegmentID creates a segment identifier that matches the actual Message-ID format
func (g *Generator) generateSegmentID(messageID string) string {
	// Remove angle brackets if present
	messageID = strings.Trim(messageID, "<>")
	return messageID
}

// sanitizeXML escapes XML special characters
func sanitizeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// sanitizeFileName removes invalid characters from filename
func sanitizeFileName(name string) string {
	reg := regexp.MustCompile(`[<>:"/\\|?*]`)
	return reg.ReplaceAllString(name, "_")
}