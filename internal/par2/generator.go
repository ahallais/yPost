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
	"unsafe"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/exp/mmap"
)

// Reed-Solomon implementation using klauspost/reedsolomon
import "github.com/klauspost/reedsolomon"

// Generator handles PAR2 recovery file generation
type Generator struct {
	par2Path string
	progress func(phase string, completed, total int64)
}

// SetProgressCallback registers an optional callback for PAR2 phase progress.
func (g *Generator) SetProgressCallback(callback func(phase string, completed, total int64)) {
	g.progress = callback
}

func (g *Generator) reportProgress(phase string, completed, total int64) {
	if g.progress != nil {
		g.progress(phase, completed, total)
	}
}

const recoveryDataShardsPerBatch = 128

type recoverySpool struct {
	file      *os.File
	blocks    int
	sliceSize int
}

// NewGenerator creates a new PAR2 generator
func NewGenerator(par2Path string) *Generator {
	return &Generator{
		par2Path: par2Path,
	}
}

// CreatePAR2ForParts creates PAR2 recovery files for split file parts (standard practice)
func (g *Generator) CreatePAR2ForParts(parts []string, baseName string, redundancy int) ([]string, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("no parts provided")
	}

	fmt.Printf("Creating PAR2 recovery files for %d parts of: %s\n", len(parts), baseName)
	fmt.Printf("Redundancy: %d%%\n", redundancy)

	// Calculate total size of all parts
	var totalSize int64
	for _, partPath := range parts {
		if info, err := os.Stat(partPath); err == nil {
			totalSize += info.Size()
		}
	}

	// Use a reasonable slice size for the parts
	sliceSize := g.calculateSliceSize(totalSize)

	// Create main PAR2 index file
	baseNameWithoutExt := baseName
	if ext := filepath.Ext(baseName); ext != "" {
		baseNameWithoutExt = baseName[:len(baseName)-len(ext)]
	}
	par2File := filepath.Join(g.par2Path, fmt.Sprintf("%s.par2", baseNameWithoutExt))

	// Generate recovery data from all parts using Reed-Solomon
	recoveryData, err := g.generateRecoveryDataBatched(parts, sliceSize, redundancy, "Reed-Solomon encoding (parts)")
	if err != nil {
		return nil, fmt.Errorf("failed to generate recovery data: %w", err)
	}
	defer recoveryData.close()

	// Write main PAR2 index file (control file with file list)
	err = g.writePAR2IndexFileForParts(par2File, parts, sliceSize, totalSize)
	if err != nil {
		return nil, fmt.Errorf("failed to write PAR2 index file: %w", err)
	}

	var par2Files []string
	par2Files = append(par2Files, par2File)

	// Create VOL files with recovery blocks following standard naming
	volFiles, err := g.createStandardVOLFilesFromSpool(baseNameWithoutExt, recoveryData)
	if err != nil {
		return nil, fmt.Errorf("failed to create VOL files: %w", err)
	}
	par2Files = append(par2Files, volFiles...)

	fmt.Printf("PAR2 recovery files created successfully: %d files\n", len(par2Files))
	return par2Files, nil
}

// CreatePAR2 creates PAR2 recovery files for the given file parts
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

	// Create main PAR2 index file
	baseName := filepath.Base(filePath)
	baseNameWithoutExt := baseName[:len(baseName)-len(filepath.Ext(baseName))]
	par2File := filepath.Join(g.par2Path, fmt.Sprintf("%s.par2", baseNameWithoutExt))

	// Generate recovery data using Reed-Solomon
	recoveryData, err := g.generateRecoveryDataBatched([]string{filePath}, sliceSize, redundancy, "Reed-Solomon encoding")
	if err != nil {
		return nil, fmt.Errorf("failed to generate recovery data: %w", err)
	}
	defer recoveryData.close()

	// Write main PAR2 index file (small control file)
	err = g.writePAR2IndexFile(par2File, filePath, sliceSize, numSlices)
	if err != nil {
		return nil, fmt.Errorf("failed to write PAR2 index file: %w", err)
	}

	var par2Files []string
	par2Files = append(par2Files, par2File)

	// Create VOL files with recovery blocks following standard naming
	volFiles, err := g.createStandardVOLFilesFromSpool(baseNameWithoutExt, recoveryData)
	if err != nil {
		return nil, fmt.Errorf("failed to create VOL files: %w", err)
	}
	par2Files = append(par2Files, volFiles...)

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

func (s *recoverySpool) close() {
	name := s.file.Name()
	_ = s.file.Close()
	_ = os.Remove(name)
}

// fileSequenceReader presents a set of split files as one continuous stream
// without keeping every file descriptor open at once.
type fileSequenceReader struct {
	paths   []string
	next    int
	current *os.File
}

func (r *fileSequenceReader) Read(p []byte) (int, error) {
	for {
		if r.current == nil {
			if r.next == len(r.paths) {
				return 0, io.EOF
			}
			file, err := os.Open(r.paths[r.next])
			if err != nil {
				return 0, fmt.Errorf("open part %s: %w", r.paths[r.next], err)
			}
			r.next++
			r.current = file
		}

		n, err := r.current.Read(p)
		if err == io.EOF {
			_ = r.current.Close()
			r.current = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (r *fileSequenceReader) Close() error {
	if r.current != nil {
		return r.current.Close()
	}
	return nil
}

// generateRecoveryDataBatched encodes independent groups and spools their
// parity to disk. Peak shard memory is bounded by
// recoveryDataShardsPerBatch rather than the total input size.
func (g *Generator) generateRecoveryDataBatched(paths []string, sliceSize int, redundancy int, description string) (*recoverySpool, error) {
	var totalSize int64
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat part %s: %w", path, err)
		}
		totalSize += info.Size()
	}

	totalDataShards := int((totalSize + int64(sliceSize) - 1) / int64(sliceSize))
	totalBatches := (totalDataShards + recoveryDataShardsPerBatch - 1) / recoveryDataShardsPerBatch
	totalParityShards := totalDataShards * redundancy / 100
	// Every independent group needs at least one recovery shard.
	if totalParityShards < totalBatches {
		totalParityShards = totalBatches
	}
	fmt.Printf("Reed-Solomon encoding in batches: %d data shards, %d parity shards, at most %d data shards per batch\n",
		totalDataShards, totalParityShards, recoveryDataShardsPerBatch)

	spoolFile, err := os.CreateTemp(g.par2Path, ".ypost-recovery-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create recovery spool: %w", err)
	}
	spool := &recoverySpool{file: spoolFile, blocks: totalParityShards, sliceSize: sliceSize}
	fail := func(err error) (*recoverySpool, error) {
		spool.close()
		return nil, err
	}

	reader := &fileSequenceReader{paths: paths}
	defer reader.Close()
	bar := progressbar.NewOptions(totalDataShards+totalParityShards,
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionThrottle(200*time.Millisecond),
	)
	progressTotal := totalDataShards + totalParityShards
	g.reportProgress("PAR2 encoding", 0, int64(progressTotal))

	dataDone, parityDone := 0, 0
	for dataDone < totalDataShards {
		dataInBatch := min(recoveryDataShardsPerBatch, totalDataShards-dataDone)
		parityThroughBatch := (dataDone + dataInBatch) * totalParityShards / totalDataShards
		parityInBatch := parityThroughBatch - parityDone
		if parityInBatch < 1 {
			parityInBatch = 1
		}

		enc, err := reedsolomon.New(dataInBatch, parityInBatch)
		if err != nil {
			return fail(fmt.Errorf("create encoder for batch: %w", err))
		}
		shards := make([][]byte, dataInBatch+parityInBatch)
		for i := 0; i < dataInBatch; i++ {
			shards[i] = make([]byte, sliceSize)
			_, err := io.ReadFull(reader, shards[i])
			if err != nil && err != io.ErrUnexpectedEOF {
				return fail(fmt.Errorf("read data shard %d: %w", dataDone+i, err))
			}
			_ = bar.Add(1)
			g.reportProgress("PAR2 encoding", int64(dataDone+i+1+parityDone), int64(progressTotal))
		}
		for i := dataInBatch; i < len(shards); i++ {
			shards[i] = make([]byte, sliceSize)
		}
		if dataDone+dataInBatch == totalDataShards {
			fmt.Println("Finalizing the last PAR2 Reed-Solomon batch; this can take some time on a slower NAS")
		}
		if err := enc.Encode(shards); err != nil {
			return fail(fmt.Errorf("encode batch starting at shard %d: %w", dataDone, err))
		}
		for parityIndex, parity := range shards[dataInBatch:] {
			if _, err := spoolFile.Write(parity); err != nil {
				return fail(fmt.Errorf("write recovery spool: %w", err))
			}
			_ = bar.Add(1)
			g.reportProgress("PAR2 encoding", int64(dataDone+dataInBatch+parityDone+parityIndex+1), int64(progressTotal))
		}
		dataDone += dataInBatch
		parityDone += parityInBatch
	}
	_ = bar.Finish()
	spool.blocks = parityDone
	if _, err := spoolFile.Seek(0, io.SeekStart); err != nil {
		return fail(fmt.Errorf("rewind recovery spool: %w", err))
	}
	return spool, nil
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

	// Read all data from mmap reader
	data := make([]byte, reader.Len())
	_, err = reader.ReadAt(data, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to read mmap data: %w", err)
	}

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
			recoverySlice := recoveryData[recoveryIndex*sliceSize : (recoveryIndex+1)*sliceSize]
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
		recoverySlice := recoveryData[i*sliceSize : (i+1)*sliceSize]

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

// generateRecoveryDataReedSolomon uses Reed-Solomon encoding for recovery data generation
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

	fmt.Printf("Reed-Solomon encoding: %d data shards, %d parity shards\n", numSlices, parityShards)

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

	// Create progress bar
	progressBar := progressbar.NewOptions(numSlices+parityShards,
		progressbar.OptionSetDescription("Reed-Solomon encoding"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionThrottle(200*time.Millisecond),
	)

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
		progressBar.Add(1)
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

	// Update progress for parity generation
	progressBar.Add(parityShards)
	progressBar.Finish()

	// Combine parity shards into recovery data
	recoveryData := make([]byte, parityShards*sliceSize)
	for i := 0; i < parityShards; i++ {
		copy(recoveryData[i*sliceSize:(i+1)*sliceSize], shards[numSlices+i])
	}

	return recoveryData, nil
}

// generateRecoveryDataReedSolomonFromParts creates Reed-Solomon recovery data from multiple file parts
func (g *Generator) generateRecoveryDataReedSolomonFromParts(parts []string, sliceSize int, redundancy int) ([]byte, error) {
	// Calculate total size of all parts
	var totalSize int64
	for _, partPath := range parts {
		if info, err := os.Stat(partPath); err == nil {
			totalSize += info.Size()
		}
	}

	numSlices := int((totalSize + int64(sliceSize) - 1) / int64(sliceSize))

	// Calculate parity shards based on redundancy
	parityShards := int(float64(numSlices) * float64(redundancy) / 100.0)
	if parityShards < 1 {
		parityShards = 1
	}

	fmt.Printf("Reed-Solomon encoding from parts: %d data shards, %d parity shards\n", numSlices, parityShards)

	// Create Reed-Solomon encoder
	enc, err := reedsolomon.New(numSlices, parityShards)
	if err != nil {
		return nil, fmt.Errorf("failed to create Reed-Solomon encoder: %w", err)
	}

	// Create progress bar
	progressBar := progressbar.NewOptions(numSlices+parityShards,
		progressbar.OptionSetDescription("Reed-Solomon encoding (parts)"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionThrottle(200*time.Millisecond),
	)

	// Create shards
	shards := make([][]byte, numSlices+parityShards)

	// Read data from all parts into shards
	shardIndex := 0
	for _, partPath := range parts {
		file, err := os.Open(partPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open part %s: %w", partPath, err)
		}

		// Read this part into shards
		for {
			if shardIndex >= numSlices {
				break
			}

			shards[shardIndex] = make([]byte, sliceSize)
			n, err := file.Read(shards[shardIndex])
			if err == io.EOF {
				break
			}
			if err != nil {
				file.Close()
				return nil, fmt.Errorf("failed to read shard from part %s: %w", partPath, err)
			}

			// Pad with zeros if needed
			if n < sliceSize {
				for j := n; j < sliceSize; j++ {
					shards[shardIndex][j] = 0
				}
			}

			shardIndex++
			progressBar.Add(1)
		}

		file.Close()
	}

	// Initialize remaining parity shards
	for i := numSlices; i < numSlices+parityShards; i++ {
		shards[i] = make([]byte, sliceSize)
	}

	// Generate parity data
	err = enc.Encode(shards)
	if err != nil {
		return nil, fmt.Errorf("failed to encode shards: %w", err)
	}

	// Update progress for parity generation
	progressBar.Add(parityShards)
	progressBar.Finish()

	// Combine parity shards into recovery data
	recoveryData := make([]byte, parityShards*sliceSize)
	for i := 0; i < parityShards; i++ {
		copy(recoveryData[i*sliceSize:(i+1)*sliceSize], shards[numSlices+i])
	}

	return recoveryData, nil
}

// writePAR2IndexFile writes the main PAR2 index file (control file)
func (g *Generator) writePAR2IndexFile(par2File string, originalFile string, sliceSize int, numSlices int) error {
	file, err := os.Create(par2File)
	if err != nil {
		return fmt.Errorf("failed to create PAR2 index file: %w", err)
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

	return nil
}

// writePAR2VolumeFile writes a PAR2 volume file with recovery data
func (g *Generator) writePAR2VolumeFile(volFile string, originalFile string, sliceSize int, numSlices int, recoveryData []byte) error {
	file, err := os.Create(volFile)
	if err != nil {
		return fmt.Errorf("failed to create PAR2 volume file: %w", err)
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

// createStandardVOLFiles creates PAR2 volume files following standard naming convention
func (g *Generator) createStandardVOLFiles(baseName string, recoveryData []byte, sliceSize int, redundancy int) ([]string, error) {
	var volFiles []string

	// Calculate how many recovery blocks we have
	totalRecoveryBlocks := len(recoveryData) / sliceSize
	if totalRecoveryBlocks == 0 {
		return volFiles, nil
	}

	// Create volume files following standard PAR2 naming: file.vol000+01.par2, file.vol001+02.par2, etc.
	// This creates a series of volumes with increasing block counts (powers of 2 pattern)
	blockIndex := 0
	volIndex := 0

	// Create progress bar for VOL file creation
	volBar := progressbar.NewOptions(totalRecoveryBlocks,
		progressbar.OptionSetDescription("Creating PAR2 volumes"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionThrottle(100*time.Millisecond),
	)

	for blockIndex < totalRecoveryBlocks {
		// Calculate blocks for this volume (start with 1, then powers of 2: 1, 2, 4, 8, ...)
		var blocksInVolume int
		if volIndex == 0 {
			blocksInVolume = 1
		} else {
			blocksInVolume = 1 << (volIndex - 1) // Powers of 2: 1, 2, 4, 8, 16...
		}

		// Don't exceed remaining blocks
		if blockIndex+blocksInVolume > totalRecoveryBlocks {
			blocksInVolume = totalRecoveryBlocks - blockIndex
		}

		// Create volume file name
		volFile := filepath.Join(g.par2Path, fmt.Sprintf("%s.vol%03d+%02d.par2", baseName, blockIndex, blocksInVolume))

		// Extract recovery data for this volume
		volumeData := recoveryData[blockIndex*sliceSize : (blockIndex+blocksInVolume)*sliceSize]

		// Write volume file
		err := g.writeVolumeFile(volFile, volumeData)
		if err != nil {
			return nil, fmt.Errorf("failed to write volume file %s: %w", volFile, err)
		}

		volFiles = append(volFiles, volFile)
		blockIndex += blocksInVolume
		volIndex++

		volBar.Add(blocksInVolume)
	}

	volBar.Finish()
	return volFiles, nil
}

// createStandardVOLFilesFromSpool creates the same volume layout while only
// holding one recovery block in memory. The recovery spool is consumed once.
func (g *Generator) createStandardVOLFilesFromSpool(baseName string, recovery *recoverySpool) ([]string, error) {
	if recovery.blocks == 0 {
		return nil, nil
	}
	if _, err := recovery.file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind recovery spool: %w", err)
	}

	volBar := progressbar.NewOptions(recovery.blocks,
		progressbar.OptionSetDescription("Creating PAR2 volumes"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionThrottle(100*time.Millisecond),
	)
	g.reportProgress("PAR2 volumes", 0, int64(recovery.blocks))

	var volFiles []string
	blockIndex, volIndex := 0, 0
	for blockIndex < recovery.blocks {
		blocksInVolume := 1
		if volIndex > 0 {
			blocksInVolume = 1 << (volIndex - 1)
		}
		if remaining := recovery.blocks - blockIndex; blocksInVolume > remaining {
			blocksInVolume = remaining
		}

		volFile := filepath.Join(g.par2Path, fmt.Sprintf("%s.vol%03d+%02d.par2", baseName, blockIndex, blocksInVolume))
		file, err := os.Create(volFile)
		if err != nil {
			return nil, fmt.Errorf("create volume file %s: %w", volFile, err)
		}
		if _, err = file.Write([]byte("PAR2\x00PKT")); err == nil {
			_, err = io.CopyN(file, recovery.file, int64(blocksInVolume)*int64(recovery.sliceSize))
		}
		closeErr := file.Close()
		if err != nil {
			return nil, fmt.Errorf("write volume file %s: %w", volFile, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close volume file %s: %w", volFile, closeErr)
		}

		volFiles = append(volFiles, volFile)
		blockIndex += blocksInVolume
		volIndex++
		_ = volBar.Add(blocksInVolume)
		g.reportProgress("PAR2 volumes", int64(blockIndex), int64(recovery.blocks))
	}
	_ = volBar.Finish()
	return volFiles, nil
}

// writeVolumeFile writes a PAR2 volume file with just the recovery data
func (g *Generator) writeVolumeFile(volFile string, recoveryData []byte) error {
	file, err := os.Create(volFile)
	if err != nil {
		return fmt.Errorf("failed to create volume file: %w", err)
	}
	defer file.Close()

	// Write PAR2 header
	header := []byte("PAR2\x00PKT")
	if _, err := file.Write(header); err != nil {
		return fmt.Errorf("failed to write PAR2 header: %w", err)
	}

	// Write recovery data
	if _, err := file.Write(recoveryData); err != nil {
		return fmt.Errorf("failed to write recovery data: %w", err)
	}

	return nil
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

// generateRecoveryDataFromParts creates recovery data from multiple file parts
func (g *Generator) generateRecoveryDataFromParts(parts []string, sliceSize int, redundancy int) ([]byte, error) {
	// Calculate total size of all parts
	var totalSize int64
	for _, partPath := range parts {
		if info, err := os.Stat(partPath); err == nil {
			totalSize += info.Size()
		}
	}

	numSlices := int((totalSize + int64(sliceSize) - 1) / int64(sliceSize))

	// Calculate recovery size based on redundancy
	recoverySlices := int(float64(numSlices) * float64(redundancy) / 100.0)
	if recoverySlices < 1 {
		recoverySlices = 1
	}

	fmt.Printf("Generating recovery data: %d slices, %d recovery slices\n", numSlices, recoverySlices)

	// Create progress bar
	progressBar := progressbar.NewOptions(recoverySlices,
		progressbar.OptionSetDescription("Generating recovery data"),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(15),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionThrottle(200*time.Millisecond),
	)

	recoveryData := make([]byte, recoverySlices*sliceSize)

	// Process each recovery slice
	for i := 0; i < recoverySlices; i++ {
		recoverySlice := recoveryData[i*sliceSize : (i+1)*sliceSize]

		// Clear recovery slice
		for j := range recoverySlice {
			recoverySlice[j] = 0
		}

		// XOR data from all parts
		sliceOffset := 0
		for _, partPath := range parts {
			err := g.xorPartIntoRecoverySlice(partPath, sliceOffset, sliceSize, numSlices, recoverySlice)
			if err != nil {
				return nil, fmt.Errorf("failed to process part %s: %w", partPath, err)
			}

			// Update slice offset for next part
			if info, err := os.Stat(partPath); err == nil {
				partSlices := int((info.Size() + int64(sliceSize) - 1) / int64(sliceSize))
				sliceOffset += partSlices
			}
		}

		progressBar.Add(1)
	}

	progressBar.Finish()
	return recoveryData, nil
}

// xorPartIntoRecoverySlice XORs data from a part file into the recovery slice
func (g *Generator) xorPartIntoRecoverySlice(partPath string, sliceOffset int, sliceSize int, totalSlices int, recoverySlice []byte) error {
	file, err := os.Open(partPath)
	if err != nil {
		return fmt.Errorf("failed to open part file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat part file: %w", err)
	}

	fileSize := fileInfo.Size()
	partSlices := int((fileSize + int64(sliceSize) - 1) / int64(sliceSize))

	// Read and XOR each slice from this part
	for i := 0; i < partSlices; i++ {
		slice := make([]byte, sliceSize)
		n, err := file.Read(slice)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read slice: %w", err)
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

	return nil
}

// writePAR2IndexFileForParts writes the main PAR2 index file for multiple parts
func (g *Generator) writePAR2IndexFileForParts(par2File string, parts []string, sliceSize int, totalSize int64) error {
	file, err := os.Create(par2File)
	if err != nil {
		return fmt.Errorf("failed to create PAR2 index file: %w", err)
	}
	defer file.Close()

	// Write PAR2 header
	header := []byte("PAR2\x00PKT")
	if _, err := file.Write(header); err != nil {
		return fmt.Errorf("failed to write PAR2 header: %w", err)
	}

	const hashBufferSize = 256 * 1024
	hashBuffer := make([]byte, hashBufferSize)
	hashedBytes := int64(0)
	g.reportProgress("PAR2 index hashing", 0, totalSize)

	// Write file descriptions for all parts
	for _, partPath := range parts {
		fileInfo, err := os.Stat(partPath)
		if err != nil {
			return fmt.Errorf("stat part %s for PAR2 index: %w", partPath, err)
		}

		part, err := os.Open(partPath)
		if err != nil {
			return fmt.Errorf("open part %s for PAR2 index: %w", partPath, err)
		}
		hash := sha256.New()
		for {
			n, readErr := part.Read(hashBuffer)
			if n > 0 {
				if _, err := hash.Write(hashBuffer[:n]); err != nil {
					_ = part.Close()
					return fmt.Errorf("hash part %s for PAR2 index: %w", partPath, err)
				}
				hashedBytes += int64(n)
				g.reportProgress("PAR2 index hashing", hashedBytes, totalSize)
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				_ = part.Close()
				return fmt.Errorf("read part %s for PAR2 index: %w", partPath, readErr)
			}
		}
		if err := part.Close(); err != nil {
			return fmt.Errorf("close part %s after PAR2 index hashing: %w", partPath, err)
		}
		fileHash := hash.Sum(nil)
		numSlices := int((fileInfo.Size() + int64(sliceSize) - 1) / int64(sliceSize))

		// Create file description for this part
		desc := g.createFileDescription(partPath, fileInfo.Size(), sliceSize, numSlices, fileHash)
		if _, err := file.Write(desc); err != nil {
			return fmt.Errorf("failed to write file description for %s: %w", partPath, err)
		}
	}
	g.reportProgress("PAR2 index hashing", totalSize, totalSize)

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
