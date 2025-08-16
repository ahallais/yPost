package par2

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
)

// Generator handles PAR2 recovery file generation
type Generator struct {
	par2Path string
}

// NewGenerator creates a new PAR2 generator
func NewGenerator(par2Path string) *Generator {
	return &Generator{
		par2Path: par2Path,
	}
}

// CreatePAR2 creates PAR2 recovery files for the given file
func (g *Generator) CreatePAR2(filePath string, redundancy int) ([]string, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	fmt.Printf("Creating PAR2 recovery files for: %s\n", fileInfo.Name())
	fmt.Printf("File size: %d bytes, Redundancy: %d%%\n", fileInfo.Size(), redundancy)

	// Calculate recovery slice parameters
	fileSize := fileInfo.Size()
	sliceSize := g.calculateSliceSize(fileSize)
	numSlices := int((fileSize + int64(sliceSize) - 1) / int64(sliceSize))

	// Create PAR2 file
	par2File := filepath.Join(filepath.Dir(filePath), fmt.Sprintf("%s.par2", fileInfo.Name()))
	
	// Generate recovery data
	recoveryData, err := g.generateRecoveryData(filePath, sliceSize, redundancy)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recovery data: %w", err)
	}

	// Write PAR2 file
	err = g.writePAR2File(par2File, filePath, sliceSize, numSlices, recoveryData)
	if err != nil {
		return nil, fmt.Errorf("failed to write PAR2 file: %w", err)
	}

	// Create additional recovery volumes if needed
	var par2Files []string
	par2Files = append(par2Files, par2File)

	// Create VOL files for additional redundancy
	if redundancy > 10 {
		volFiles, err := g.createVOLFiles(filePath, sliceSize, numSlices, redundancy)
		if err != nil {
			return nil, fmt.Errorf("failed to create VOL files: %w", err)
		}
		par2Files = append(par2Files, volFiles...)
	}

	fmt.Printf("PAR2 recovery files created successfully: %d files\n", len(par2Files))
	return par2Files, nil
}

// calculateSliceSize determines appropriate slice size based on file size
func (g *Generator) calculateSliceSize(fileSize int64) int {
	// Use different slice sizes based on file size
	switch {
	case fileSize < 1024*1024: // < 1MB
		return 4 * 1024 // 4KB
	case fileSize < 100*1024*1024: // < 100MB
		return 64 * 1024 // 64KB
	case fileSize < 1024*1024*1024: // < 1GB
		return 256 * 1024 // 256KB
	default:
		return 512 * 1024 // 512KB
	}
}

// generateRecoveryData creates recovery data using Reed-Solomon-like algorithm
func (g *Generator) generateRecoveryData(filePath string, sliceSize int, redundancy int) ([]byte, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}
	fileSize := fileInfo.Size()
	numSlices := int((fileSize + int64(sliceSize) - 1) / int64(sliceSize))

	// Generate recovery data (simplified XOR-based approach)
	recoverySize := int(float64(numSlices) * float64(redundancy) / 100.0)
	if recoverySize < 1 {
		recoverySize = 1
	}

	// Create progress bar for recovery data generation
	totalOperations := recoverySize * sliceSize
	recoveryBar := progressbar.NewOptions(totalOperations,
		progressbar.OptionSetDescription("Generating recovery data"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionThrottle(100*time.Millisecond),
	)

	// Memory-efficient approach: compute XOR incrementally without loading all slices
	recoveryData := make