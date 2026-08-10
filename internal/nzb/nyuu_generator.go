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
	tempPath     string
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

	file, err := os.CreateTemp(filepath.Dir(outputPath), "."+filepath.Base(outputPath)+".*.part")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary NZB file: %w", err)
	}
	_ = file.Chmod(0644)

	ng := &NyuuGenerator{
		outputPath: outputPath,
		tempPath:   file.Name(),
		poster:     poster,
		file:       file,
	}

	// Write NZB header exactly like Nyuu (compact format)
	if err := ng.writeHeader(); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, err
	}

	return ng, nil
}

// writeHeader writes the NZB XML header in Nyuu's compact format
func (ng *NyuuGenerator) writeHeader() error {
	// Exact format from working Nyuu NZB
	header := `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd"><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">`
	_, err := ng.file.WriteString(header)
	return err
}

// StartFile begins a new file entry in the NZB (Nyuu format)
func (ng *NyuuGenerator) StartFile(fileName string, groups []string, date time.Time) error {
	ng.mutex.Lock()
	defer ng.mutex.Unlock()
	if ng.file == nil {
		return fmt.Errorf("NZB generator is closed")
	}

	ng.segmentCount = 0

	// Write file header in Nyuu's compact format
	fileHeader := fmt.Sprintf(`<file poster="%s" date="%d" subject="%s"><groups>`,
		xmlText(ng.poster), date.Unix(), xmlText(fileName))

	if _, err := ng.file.WriteString(fileHeader); err != nil {
		return err
	}

	// Write groups in compact format
	for _, group := range groups {
		if _, err := ng.file.WriteString(fmt.Sprintf("<group>%s</group>", xmlText(group))); err != nil {
			return err
		}
	}

	_, err := ng.file.WriteString("</groups><segments>")
	return err
}

// AddSegment adds a segment to the current file (Nyuu format)
func (ng *NyuuGenerator) AddSegment(messageID string, bytes int64) error {
	ng.mutex.Lock()
	defer ng.mutex.Unlock()
	if ng.file == nil {
		return fmt.Errorf("NZB generator is closed")
	}

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
	if ng.file == nil {
		return fmt.Errorf("NZB generator is closed")
	}
	_, err := ng.file.WriteString("</segments></file>")
	return err
}

// Close finalizes and closes the NZB file
func (ng *NyuuGenerator) Close() error {
	ng.mutex.Lock()
	defer ng.mutex.Unlock()
	if ng.file == nil {
		return nil
	}

	if _, err := ng.file.WriteString("</nzb>"); err != nil {
		return err
	}
	if err := ng.file.Sync(); err != nil {
		return err
	}
	if err := ng.file.Close(); err != nil {
		ng.file = nil
		return err
	}
	ng.file = nil
	if err := os.Rename(ng.tempPath, ng.outputPath); err != nil {
		return fmt.Errorf("publish NZB: %w", err)
	}
	return nil
}

// Abort closes and removes the temporary NZB without publishing a partial
// document at the final output path.
func (ng *NyuuGenerator) Abort() error {
	ng.mutex.Lock()
	defer ng.mutex.Unlock()
	var closeErr error
	if ng.file != nil {
		closeErr = ng.file.Close()
		ng.file = nil
	}
	removeErr := os.Remove(ng.tempPath)
	if os.IsNotExist(removeErr) {
		removeErr = nil
	}
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

// GetPath returns the output path
func (ng *NyuuGenerator) GetPath() string {
	return ng.outputPath
}
