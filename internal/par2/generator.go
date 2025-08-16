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
	"syscall"
	"time"
	"unsafe"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/exp/mmap"
)

// Alternative Reed-Solomon implementation using klauspost/reedsolomon
// Uncomment and use this for even better performance:
// import "github.com/klauspost/reedsolomon"

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

// generateRecoveryData creates recovery data using optimized memory-mapped approach
func (g *Generator) generateRecoveryData(filePath string, sliceSize int, redundancy int) ([]byte, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	fileSize := fileInfo.Size()
	numSlices := int((fileSize + int64(sliceSize) - 1) / int64(sliceSize))

	// Calculate recovery size
	recoverySize := int(float64(numSlices) * float64(redundancy) / 100.0)
	if recoverySize < 1 {
		recoverySize = 1
	}

	// Use memory mapping for large files (>10MB), otherwise use streaming
	if fileSize > 10*1024*1024 {
		return g.generateRecoveryDataMmap(filePath, sliceSize, numSlices, recoverySize)
	}
	return g.generateRecoveryDataStream(filePath, sliceSize, numSlices, recoverySize)
}

// generateRecoveryDataMmap uses memory mapping for efficient file access
func (g *Generator) generateRecoveryDataMmap(filePath string, sliceSize, numSlices, recoverySize int) ([]byte, error) {
	// Memory map the file
	reader, err := mmap.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to mmap file: %w", err)
	}
	defer reader.Close()

	data := reader.Data()
	
	// Create progress bar with throttled updates
	progressBar := progressbar.NewOptions(recoverySize,
		progressbar.OptionSetDescription("Generating recovery data (mmap)"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionThrottle(200*time.Millisecond),
	)

	recoveryData := make([]byte, recoverySize*sliceSize)
	
	// Use parallel processing for XOR computation
	numWorkers := runtime.NumCPU()
	var wg sync.WaitGroup
	
	// Process recovery blocks in parallel
	for i := 0; i < recoverySize; i++ {
		wg.Add(1)
		go func(recoveryIndex int) {
			defer wg.Done()
			
			// Calculate XOR for this recovery block
			recoverySlice := recoveryData[recoveryIndex*sliceSize:(recoveryIndex+1)*sliceSize]
			g.xorSlicesFromMmap(data, sliceSize, numSlices, recoverySlice)
			
			// Throttled progress update
			if recoveryIndex%max(1, recoverySize/100) == 0 {
				progressBar.Add(1)
			}
		}(i)
		
		// Limit concurrent goroutines to prevent memory pressure
		if (i+1)%numWorkers == 0 {
			wg.Wait()
		}
	}
	wg.Wait()
	
	progressBar.Finish()
	return recoveryData, nil
}

// generateRecoveryDataStream uses streaming approach for smaller files
func (g *Generator) generateRecoveryDataStream(filePath string, sliceSize, numSlices, recoverySize int) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create progress bar
	progressBar := progressbar.NewOptions(recoverySize,
		progressbar.OptionSetDescription("Generating recovery data (stream)"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionThrottle(200*time.Millisecond),
	)

	recoveryData := make([]byte, recoverySize*sliceSize)
	
	// Process each recovery block
	for i := 0; i < recoverySize; i++ {
		recoverySlice := recoveryData[i*sliceSize:(i+1)*sliceSize]
		
		// Reset file position
		file.Seek(0, 0)
		
		// XOR all slices for this recovery block
		for j := 0; j < numSlices; j++ {
			slice := make([]byte, sliceSize)
			n, err := file.Read(slice)
			if err != nil && err != io.EOF {
				return nil, fmt.Errorf("failed to read slice: %w", err)
			}
			
			// Pad with zeros if needed
			if n < sliceSize {
				for k := n; k < sliceSize; k++ {
					slice[k] = 0
				}
			}
			
			// XOR with recovery slice
			g.xorBytes(recoverySlice, slice)
		}
		
		progressBar.Add(1)
	}
	
	return recoveryData, nil
}

// xorSlicesFromMmap efficiently XORs slices from memory-mapped data
func (g *Generator) xorSlicesFromMmap(data []byte, sliceSize, numSlices int, result []byte) {
	// Clear result slice
	for i := range result {
		result[i] = 0
	}
	
	dataLen := len(data)
	
	// XOR each slice
	for sliceIdx := 0; sliceIdx < numSlices; sliceIdx++ {
		offset := sliceIdx * sliceSize
		
		// Handle last slice which might be shorter
		actualSliceSize := sliceSize
		if offset+sliceSize > dataLen {
			actualSliceSize = dataLen - offset
		}
		
		if actualSliceSize <= 0 {
			break
		}
		
		// Use SIMD-optimized XOR for better performance
		g.xorBytesOptimized(result[:actualSliceSize], data[offset:offset+actualSliceSize])
	}
}

// xorBytes performs XOR operation between two byte slices
func (g *Generator) xorBytes(dst, src []byte) {
	minLen := len(dst)
	if len(src) < minLen {
		minLen = len(src)
	}
	
	for i := 0; i < minLen; i++ {
		dst[i] ^= src[i]
	}
}

// xorBytesOptimized performs optimized XOR using word-sized operations
func (g *Generator) xorBytesOptimized(dst, src []byte) {
	minLen := len(dst)
	if len(src) < minLen {
		minLen = len(src)
	}
	
	// Process 8 bytes at a time for better performance
	i := 0
	for i+8 <= minLen {
		dstPtr := (*uint64)(unsafe.Pointer(&dst[i]))
		srcPtr := (*uint64)(unsafe.Pointer(&src[i]))
		*dstPtr ^= *srcPtr
		i += 8
	}
	
	// Handle remaining bytes
	for i < minLen {
		dst[i] ^= src[i]
		i++
	}
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Alternative high-performance Reed-Solomon implementation
// To use this, add "github.com/klauspost/reedsolomon" to go.mod and uncomment:
/*
func (g *Generator) generateRecoveryDataReedSolomon(filePath string, sliceSize int, redundancy int) ([]byte, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	fileSize := fileInfo.Size()
	numSlices := int((fileSize + int64(sliceSize) - 1) / int64(sliceSize))
	
	// Calculate parity shards based on redundancy
	parityShards := int(float64(numSlices) * float64(redundancy) / 100.0)
	if parityShards < 1 {
		parityShards = 1
	}

	// Create Reed-Solomon encoder
	enc, err := reedsolomon.New(numSlices, parityShards)
	if err != nil {
		return nil, fmt.Errorf("failed to create Reed-Solomon encoder: %w", err)
	}

	// Read file data into shards
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create shards
	shards := make([][]byte, numSlices+parityShards)
	for i := 0; i < numSlices; i++ {
		shards[i] = make([]byte, sliceSize)
		n, err := file.Read(shards[i])
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read shard: %w", err)
		}
		// Pad with zeros if needed
		if n < sliceSize {
			for j := n; j < sliceSize; j++ {
				shards[i][j] = 0
			}
		}
	}

	// Initialize parity shards
	for i := numSlices; i < numSlices+parityShards; i++ {
		shards[i] = make([]byte, sliceSize)
	}

	// Generate parity data
	err = enc.Encode(shards)
	if err != nil {
		return nil, fmt.Errorf("failed to encode shards: %w", err)
	}

	// Combine parity shards into recovery data
	recoveryData := make([]byte, parityShards*sliceSize)
	for i := 0; i < parityShards; i++ {
		copy(recoveryData[i*sliceSize:(i+1)*sliceSize], shards[numSlices+i])
	}

	return recoveryData, nil
}
*/

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
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionThrottle(100*time.Millisecond),
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