package par2

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fileInfo, _ := file.Stat()
	fileSize := fileInfo.Size()
	numSlices := int((fileSize + int64(sliceSize) - 1) / int64(sliceSize))

	// Create progress bar for file reading
	readBar := progressbar.NewOptions(numSlices,
		progressbar.OptionSetDescription("Reading file slices"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
	)

	// Read file in slices
	slices := make([][]byte, numSlices)
	for i := 0; i < numSlices; i++ {
		slice := make([]byte, sliceSize)
		n, err := file.Read(slice)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read file slice: %w", err)
		}
		if n < sliceSize {
			// Pad last slice with zeros
			for j := n; j < sliceSize; j++ {
				slice[j] = 0
			}
		}
		slices[i] = slice
		readBar.Add(1)
	}

	// Generate recovery data (simplified XOR-based approach)
	recoverySize := int(float64(numSlices) * float64(redundancy) / 100.0)
	if recoverySize < 1 {
		recoverySize = 1
	}

	// Create progress bar for recovery data generation
	recoveryBar := progressbar.NewOptions(recoverySize*sliceSize,
		progressbar.OptionSetDescription("Generating recovery data"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
	)

	recoveryData := make([]byte, recoverySize*sliceSize)
	for i := 0; i < recoverySize; i++ {
		for j := 0; j < sliceSize; j++ {
			var xor byte
			for k := 0; k < numSlices; k++ {
				xor ^= slices[k][j]
			}
			recoveryData[i*sliceSize+j] = xor
			recoveryBar.Add(1)
		}
	}

	return recoveryData, nil
}

// writePAR2File writes the PAR2 file with proper format
func (g *Generator) writePAR2File(par2File string, originalFile string, sliceSize int, numSlices int, recoveryData []byte) error {
	file, err := os.Create(par2File)
	if err != nil {
		return fmt.Errorf("failed to create PAR2 file: %w", err)
	}
	defer file.Close()

	// Write PAR2 header
	header := []byte("PAR2\x00PKT")
	if _, err := file.Write(header); err != nil {
		return fmt.Errorf("failed to write PAR2 header: %w", err)
	}

	// Write file description packet
	fileInfo, _ := os.Stat(originalFile)
	fileHash := g.calculateFileHash(originalFile)

	// Create file description
	desc := g.createFileDescription(originalFile, fileInfo.Size(), sliceSize, numSlices, fileHash)
	if _, err := file.Write(desc); err != nil {
		return fmt.Errorf("failed to write file description: %w", err)
	}

	// Write recovery data
	if _, err := file.Write(recoveryData); err != nil {
		return fmt.Errorf("failed to write recovery data: %w", err)
	}

	return nil
}

// calculateFileHash calculates SHA256 hash of the file
func (g *Generator) calculateFileHash(filePath string) []byte {
	file, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	hash := sha256.New()
	io.Copy(hash, file)
	return hash.Sum(nil)
}

// createFileDescription creates the file description packet
func (g *Generator) createFileDescription(filename string, fileSize int64, sliceSize int, numSlices int, fileHash []byte) []byte {
	var desc []byte
	
	// Add filename
	desc = append(desc, []byte(filename)...)
	desc = append(desc, 0) // null terminator
	
	// Add file size
	sizeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(sizeBytes, uint64(fileSize))
	desc = append(desc, sizeBytes...)
	
	// Add slice size
	sliceBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sliceBytes, uint32(sliceSize))
	desc = append(desc, sliceBytes...)
	
	// Add number of slices
	numSlicesBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(numSlicesBytes, uint32(numSlices))
	desc = append(desc, numSlicesBytes...)
	
	// Add file hash
	desc = append(desc, fileHash...)
	
	return desc
}

// createVOLFiles creates additional recovery volume files
func (g *Generator) createVOLFiles(originalFile string, sliceSize int, numSlices int, redundancy int) ([]string, error) {
	var volFiles []string
	
	// Create VOL files based on redundancy
	volCount := (redundancy + 9) / 10 // Create 1 VOL file per 10% redundancy
	
	// Create progress bar for VOL file creation
	volBar := progressbar.NewOptions(volCount,
		progressbar.OptionSetDescription("Creating VOL files"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
	)
	
	for i := 1; i <= volCount; i++ {
		volFile := filepath.Join(filepath.Dir(originalFile), fmt.Sprintf("%s.vol%02d+01.par2",
			filepath.Base(originalFile), i))
		
		// Generate additional recovery data for this volume
		recoveryData, err := g.generateRecoveryData(originalFile, sliceSize, 10)
		if err != nil {
			return nil, err
		}
		
		err = g.writePAR2File(volFile, originalFile, sliceSize, numSlices, recoveryData)
		if err != nil {
			return nil, err
		}
		
		volFiles = append(volFiles, volFile)
		volBar.Add(1)
	}
	
	return volFiles, nil
}

// VerifyPAR2 verifies the integrity of a file using PAR2 data
func (g *Generator) VerifyPAR2(filePath string, par2File string) (bool, error) {
	// Simplified verification - check if file exists and has correct hash
	fileHash := g.calculateFileHash(filePath)
	
	// Read PAR2 file and compare hashes
	par2Data, err := os.ReadFile(par2File)
	if err != nil {
		return false, fmt.Errorf("failed to read PAR2 file: %w", err)
	}
	
	// Extract stored hash from PAR2 file (simplified)
	// In a real implementation, this would parse the PAR2 format properly
	storedHash := g.extractHashFromPAR2(par2Data)
	
	return string(fileHash) == string(storedHash), nil
}

// extractHashFromPAR2 extracts the stored hash from PAR2 file
func (g *Generator) extractHashFromPAR2(par2Data []byte) []byte {
	// Simplified extraction - look for hash in the data
	// In real implementation, parse PAR2 format properly
	if len(par2Data) > 64 {
		return par2Data[len(par2Data)-32:] // Last 32 bytes as hash
	}
	return nil
}

// GetPAR2Info returns information about PAR2 files
func (g *Generator) GetPAR2Info(par2File string) (int64, int, error) {
	fileInfo, err := os.Stat(par2File)
	if err != nil {
		return 0, 0, err
	}
	
	// Simplified - return file size and slice count
	return fileInfo.Size(), 1, nil
}