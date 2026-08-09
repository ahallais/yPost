package nzb

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// StreamingGenerator writes NZB content as segments are posted (like Nyuu)
type StreamingGenerator struct {
	outputPath string
	poster     string
	file       *os.File
	mutex      sync.Mutex
	fileCount  int
	totalFiles int
	currentFileSegments int
}

// NewStreamingGenerator creates a new streaming NZB generator
func NewStreamingGenerator(outputPath string, poster string, totalFiles int) (*StreamingGenerator, error) {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create NZB file: %w", err)
	}

	sg := &StreamingGenerator{
		outputPath: outputPath,
		poster:     poster,
		file:       file,
		totalFiles: totalFiles,
	}

	// Write NZB header
	sg.writeHeader()
	
	return sg, nil
}

// writeHeader writes the NZB XML header
func (sg *StreamingGenerator) writeHeader() {
	header := `<?xml version="1.0" encoding="iso-8859-1"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head>
    <meta type="title">ypost</meta>
    <meta type="category">misc</meta>
    <meta type="tag">AI</meta>
  </head>
`
	sg.file.WriteString(header)
}

// StartFile begins a new file entry in the NZB
func (sg *StreamingGenerator) StartFile(fileName string, subject string, groups []string, date time.Time) error {
	sg.mutex.Lock()
	defer sg.mutex.Unlock()

	// Close previous file if any
	if sg.currentFileSegments > 0 {
		sg.file.WriteString("    </segments>\n  </file>\n")
	}

	sg.fileCount++
	sg.currentFileSegments = 0

	// Create proper subject with file indexing
	displaySubject := fmt.Sprintf("[%d/%d] - \"%s\"", sg.fileCount, sg.totalFiles, fileName)
	if subject != "" {
		displaySubject = subject
	}

	// Write file header
	fileHeader := fmt.Sprintf(`  <file poster="%s" date="%d" subject="%s">
    <groups>
`, sanitizeXML(sg.poster), date.Unix(), sanitizeXML(displaySubject))

	sg.file.WriteString(fileHeader)

	// Write groups
	for _, group := range groups {
		sg.file.WriteString(fmt.Sprintf("      <group>%s</group>\n", sanitizeXML(group)))
	}

	sg.file.WriteString("    </groups>\n    <segments>\n")
	
	return nil
}

// AddSegment adds a segment to the current file
func (sg *StreamingGenerator) AddSegment(messageID string, bytes int64) error {
	sg.mutex.Lock()
	defer sg.mutex.Unlock()

	sg.currentFileSegments++
	
	// Write segment
	segment := fmt.Sprintf("      <segment bytes=\"%d\" number=\"%d\">%s</segment>\n", 
		bytes, sg.currentFileSegments, sanitizeXML(messageID))
	
	_, err := sg.file.WriteString(segment)
	return err
}

// Close finalizes and closes the NZB file
func (sg *StreamingGenerator) Close() error {
	sg.mutex.Lock()
	defer sg.mutex.Unlock()

	// Close last file if any
	if sg.currentFileSegments > 0 {
		sg.file.WriteString("    </segments>\n  </file>\n")
	}

	// Write NZB footer
	sg.file.WriteString("</nzb>\n")
	
	return sg.file.Close()
}

// GetPath returns the output path
func (sg *StreamingGenerator) GetPath() string {
	return sg.outputPath
}