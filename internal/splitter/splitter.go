package splitter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"ypost/pkg/models"
)

// Splitter handles file splitting operations
type Splitter struct {
	maxPartSize   int64
	maxLineLength int
}

// NewSplitter creates a new file splitter
func NewSplitter(maxPartSize int64, maxLineLength int) *Splitter {
	return &Splitter{
		maxPartSize:   maxPartSize,
		maxLineLength: maxLineLength,
	}
}

// SplitFile splits a file into parts based on configuration
func (s *Splitter) SplitFile(filePath string) ([]*models.FilePart, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var parts []*models.FilePart
	partNumber := 1
	bytesRead := int64(0)

	for bytesRead < fileInfo.Size() {
		partSize := s.maxPartSize
		if fileInfo.Size()-bytesRead < partSize {
			partSize = fileInfo.Size() - bytesRead
		}

		data := make([]byte, partSize)
		n, err := file.Read(data)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}

		if n > 0 {
			data = data[:n]
			checksum := s.calculateChecksum(data)
			
			part := &models.FilePart{
				PartNumber: partNumber,
				FileName:   filepath.Base(filePath),
				Size:       int64(n),
				Data:       data,
				Checksum:   checksum,
			}
			
			parts = append(parts, part)
			partNumber++
			bytesRead += int64(n)
		}

		if err == io.EOF {
			break
		}
	}

	return parts, nil
}

// SplitIntoChunks splits data into chunks of specified size
func (s *Splitter) SplitIntoChunks(data []byte, chunkSize int64) [][]byte {
	var chunks [][]byte
	
	for i := int64(0); i < int64(len(data)); i += chunkSize {
		end := i + chunkSize
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		chunks = append(chunks, data[i:end])
	}
	
	return chunks
}

// calculateChecksum calculates SHA256 checksum for data integrity
func (s *Splitter) calculateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// GetPartFileName generates a filename for a file part
func (s *Splitter) GetPartFileName(originalName string, partNumber int, totalParts int) string {
	ext := filepath.Ext(originalName)
	base := originalName[:len(originalName)-len(ext)]
	
	if totalParts > 1 {
		return fmt.Sprintf("%s.part%02d%s", base, partNumber, ext)
	}
	
	return originalName
}

// JoinParts joins file parts back into a single file
func (s *Splitter) JoinParts(parts []*models.FilePart, outputPath string) error {
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outputFile.Close()

	for _, part := range parts {
		// Verify checksum
		calculatedChecksum := s.calculateChecksum(part.Data)
		if calculatedChecksum != part.Checksum {
			return fmt.Errorf("checksum mismatch for part %d", part.PartNumber)
		}

		_, err := outputFile.Write(part.Data)
		if err != nil {
			return fmt.Errorf("failed to write part %d: %w", part.PartNumber, err)
		}
	}

	return nil
}

// GetPartInfo returns information about file parts without splitting
func (s *Splitter) GetPartInfo(filePath string) (int64, int, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to stat file: %w", err)
	}

	fileSize := fileInfo.Size()
	totalParts := int((fileSize + s.maxPartSize - 1) / s.maxPartSize)

	return fileSize, totalParts, nil
}

// ValidateParts validates that all parts exist and have correct checksums
func (s *Splitter) ValidateParts(parts []*models.FilePart) error {
	for _, part := range parts {
		calculatedChecksum := s.calculateChecksum(part.Data)
		if calculatedChecksum != part.Checksum {
			return fmt.Errorf("checksum validation failed for part %d", part.PartNumber)
		}
	}
	return nil
}