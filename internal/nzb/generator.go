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
	
	// Split group string by comma for multiple groups
	groups := strings.Split(group, ",")
	for i := range groups {
		groups[i] = strings.TrimSpace(groups[i])
	}
	
	// Collect all files and their segments
	allFiles := []struct {
		name     string
		segments []*models.PostSegment
	}{}
	
	// Add main file
	if len(segments) > 0 {
		allFiles = append(allFiles, struct {
			name     string
			segments []*models.PostSegment
		}{fileName, segments})
	}
	
	// Add additional files (PAR2, SFV, etc.) - each as separate file entries
	for _, fileSegments := range additionalFiles {
		if len(fileSegments) > 0 {
			// Group segments by actual filename extracted from subject
			fileMap := make(map[string][]*models.PostSegment)
			for _, segment := range fileSegments {
				actualFileName := extractFilenameFromSubject(segment.Subject)
				if actualFileName == "" {
					actualFileName = segment.FileName
				}
				fileMap[actualFileName] = append(fileMap[actualFileName], segment)
			}
			
			// Add each unique file
			for name, segs := range fileMap {
				allFiles = append(allFiles, struct {
					name     string
					segments []*models.PostSegment
				}{name, segs})
			}
		}
	}
	
	totalFiles := len(allFiles)
	
	// Create file entries
	for fileIndex, file := range allFiles {
		if len(file.segments) == 0 {
			continue
		}
		
		// Use the configured poster value
		poster := g.poster
		date := time.Now().Unix()
		
		// Create subject based on the original subject pattern but with proper file indexing
		baseSubject := file.segments[0].Subject
		subject := createProperSubject(baseSubject, file.name, fileIndex+1, totalFiles)
		
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
		
		// Add segments with proper sequential numbering (1, 2, 3, ...)
		for segmentIndex, segment := range file.segments {
			segmentID := g.generateSegmentID(segment.MessageID)
			content.WriteString(fmt.Sprintf(`      <segment bytes="%d" number="%d">%s</segment>
`, segment.BytesPosted, segmentIndex+1, segmentID))
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

// extractFilenameFromSubject extracts the actual filename from the subject line
func extractFilenameFromSubject(subject string) string {
	// Look for quoted filename pattern like "filename.ext"
	re := regexp.MustCompile(`"([^"]+\.[^"]+)"`)
	matches := re.FindStringSubmatch(subject)
	if len(matches) > 1 {
		return matches[1]
	}
	
	// Look for filename pattern after " - "
	re = regexp.MustCompile(` - ([^\s]+\.[^\s]+)`)
	matches = re.FindStringSubmatch(subject)
	if len(matches) > 1 {
		return matches[1]
	}
	
	return ""
}

// extractBaseSubject extracts the base subject without part numbers
func extractBaseSubject(subject string) string {
	// Remove existing part indicators like [1/4] or (01/55)
	re := regexp.MustCompile(`^\[[^\]]+\]\s*-?\s*`)
	subject = re.ReplaceAllString(subject, "")
	
	re = regexp.MustCompile(`^\([^\)]+\)\s*-?\s*`)
	subject = re.ReplaceAllString(subject, "")
	
	return strings.TrimSpace(subject)
}

// createProperSubject creates a proper NZB subject line with correct file indexing
func createProperSubject(originalSubject, fileName string, fileIndex, totalFiles int) string {
	// Extract the base content from the original subject
	baseContent := extractBaseSubject(originalSubject)
	
	// If we can extract a filename from quotes, use it
	extractedName := extractFilenameFromSubject(originalSubject)
	if extractedName != "" {
		// Format like: [1/4] - "filename.ext" - (size) yEnc (part/total)
		return fmt.Sprintf("[%d/%d] - \"%s\"", fileIndex, totalFiles, extractedName)
	}
	
	// Otherwise use the base content
	if baseContent != "" {
		return fmt.Sprintf("[%d/%d] - %s", fileIndex, totalFiles, baseContent)
	}
	
	// Fallback to filename
	return fmt.Sprintf("[%d/%d] - %s", fileIndex, totalFiles, fileName)
}