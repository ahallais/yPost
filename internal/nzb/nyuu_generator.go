package nzb

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// NyuuGenerator creates NZB files that exactly match Nyuu's format
type NyuuGenerator struct {
	outputPath   string
	poster       string
	file         *os.File
	mutex        sync.Mutex
	segmentCount int
}

// NewNyuuGenerator creates a new Nyuu-compatible NZB generator
func NewNyuuGenerator(outputPath string, poster string) (*NyuuGenerator, error) {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create NZB file: %w", err)
	}

	ng := &NyuuGenerator{
		outputPath: outputPath,
		poster:     poster,
		file:       file,
	}

	// Write NZB header exactly like Nyuu (compact format)
	ng.writeHeader()

	return ng, nil
}

// writeHeader writes the NZB XML header in Nyuu's compact format
func (ng *NyuuGenerator) writeHeader() {
	// Exact format from working Nyuu NZB
	header := `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd"><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">`
	ng.file.WriteString(header)
}

// StartFile begins a new file entry in the NZB (Nyuu format)
func (ng *NyuuGenerator) StartFile(fileName string, groups []string, date time.Time) error {
	ng.mutex.Lock()
	defer ng.mutex.Unlock()

	ng.segmentCount = 0

	// Write file header in Nyuu's compact format
	fileHeader := fmt.Sprintf(`<file poster="%s" date="%d" subject="%s"><groups>`,
		xmlText(ng.poster), date.Unix(), xmlText(fileName))

	ng.file.WriteString(fileHeader)

	// Write groups in compact format
	for _, group := range groups {
		ng.file.WriteString(fmt.Sprintf("<group>%s</group>", xmlText(group)))
	}

	ng.file.WriteString("</groups><segments>")

	return nil
}

// AddSegment adds a segment to the current file (Nyuu format)
func (ng *NyuuGenerator) AddSegment(messageID string, bytes int64) error {
	ng.mutex.Lock()
	defer ng.mutex.Unlock()

	ng.segmentCount++

	// Write segment in Nyuu's compact format
	segment := fmt.Sprintf(`<segment bytes="%d" number="%d">%s</segment>`,
		bytes, ng.segmentCount, xmlText(messageID))

	_, err := ng.file.WriteString(segment)
	return err
}

func xmlText(value string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(value))
	return out.String()
}

// EndFile closes the current file entry
func (ng *NyuuGenerator) EndFile() error {
	ng.mutex.Lock()
	defer ng.mutex.Unlock()

	ng.file.WriteString("</segments></file>")
	return nil
}

// Close finalizes and closes the NZB file
func (ng *NyuuGenerator) Close() error {
	ng.mutex.Lock()
	defer ng.mutex.Unlock()

	// Write NZB footer
	ng.file.WriteString("</nzb>")

	return ng.file.Close()
}

// GetPath returns the output path
func (ng *NyuuGenerator) GetPath() string {
	return ng.outputPath
}
