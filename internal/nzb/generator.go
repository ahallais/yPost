package nzb

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"ypost/pkg/models"
)

// Generator handles NZB file generation
type Generator struct {
	outputDir string
}

// NewGenerator creates a new NZB generator
func NewGenerator(outputDir string) *Generator {
	return &Generator{
		outputDir: outputDir,
	}
}

// Generate creates an NZB file from posting results
func (g *Generator) Generate(fileName string, segments []*models.PostSegment, group string, additionalFiles map[string][]*models.PostSegment) (string, error) {
	if err := os.MkdirAll(g.outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	nzbFile := g.buildNZB(fileName, segments, group, additionalFiles)
	
	filePath := filepath.Join(g.outputDir, fmt.Sprintf("%s.nzb", sanitizeFileName(fileName)))
	
	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create NZB file: %w", err)
	}
	defer file.Close()

	encoder := xml.NewEncoder(file)
	encoder.Indent("", "  ")
	
	if err := encoder.Encode(nzbFile); err != nil {
		return "", fmt.Errorf("failed to encode NZB file: %w", err)
	}

	return filePath, nil
}

// buildNZB constructs the NZB XML structure
func (g *Generator) buildNZB(fileName string, segments []*models.PostSegment, group string, additionalFiles map[string][]*models.PostSegment) *models.NZBFile {
	nzb := &models.NZBFile{
		Meta: models.NZBMeta{
			Title: fileName,
		},
		Segments: make([]models.NZBSegment, 0),
	}

	if len(segments) == 0 {
		return nzb
	}

	// Create main file segment
	fileSegment := models.NZBSegment{
		Poster:   "ypost@tool.local", // Default poster
		Date:     time.Now().Unix(),
		Subject:  segments[0].Subject,
		Groups:   []string{group},
		Segments: make([]models.NZBPart, 0, len(segments)),
	}

	for _, segment := range segments {
		fileSegment.Segments = append(fileSegment.Segments, models.NZBPart{
			Bytes:     segment.BytesPosted,
			Number:    segment.PartNumber,
			MessageID: segment.MessageID,
		})
	}

	nzb.Segments = append(nzb.Segments, fileSegment)

	// Add additional files (PAR2, SFV, etc.)
	for _, fileSegments := range additionalFiles {
		if len(fileSegments) > 0 {
			additionalSegment := models.NZBSegment{
				Poster:   "ypost@tool.local",
				Date:     time.Now().Unix(),
				Subject:  fileSegments[0].Subject,
				Groups:   []string{group},
				Segments: make([]models.NZBPart, 0, len(fileSegments)),
			}

			for _, segment := range fileSegments {
				additionalSegment.Segments = append(additionalSegment.Segments, models.NZBPart{
					Bytes:     segment.BytesPosted,
					Number:    segment.PartNumber,
					MessageID: segment.MessageID,
				})
			}

			nzb.Segments = append(nzb.Segments, additionalSegment)
		}
	}

	return nzb
}

// sanitizeFileName removes invalid characters from filename
func sanitizeFileName(name string) string {
	reg := regexp.MustCompile(`[<>:"/\\|?*]`)
	return reg.ReplaceAllString(name, "_")
}