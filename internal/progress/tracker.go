package progress

import (
	"fmt"
	"sync"
	"time"
)

// Tracker handles real-time progress tracking for file transmission
type Tracker struct {
	mu           sync.Mutex
	totalChunks  int
	currentChunk int
	filename     string
	totalBytes   int64
	bytesSent    int64
	startTime    time.Time
}

// NewTracker creates a new progress tracker
func NewTracker(filename string, totalChunks int, totalBytes int64) *Tracker {
	return &Tracker{
		filename:    filename,
		totalChunks: totalChunks,
		totalBytes:  totalBytes,
		startTime:   time.Now(),
	}
}

// EmitProgress emits a progress line in the format: "Part X/Y <filename> N bytes"
func (t *Tracker) EmitProgress(chunkNum int, bytes int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	t.currentChunk = chunkNum
	t.bytesSent += bytes
	
	// Format: Part 1/12 <filename> 1024 bytes
	fmt.Printf("Part %d/%d <%s> %d bytes\n", chunkNum, t.totalChunks, t.filename, bytes)
}

// EmitComplete emits the final progress line
func (t *Tracker) EmitComplete() {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	// Emit final part if not already done
	if t.currentChunk < t.totalChunks {
		fmt.Printf("Part %d/%d <%s> %d bytes\n", t.totalChunks, t.totalChunks, t.filename, t.totalBytes-t.bytesSent)
	}
	
	duration := time.Since(t.startTime)
	fmt.Printf("Transmission complete: %s (%d bytes in %v)\n", t.filename, t.totalBytes, duration)
}

// GetProgress returns current progress information
func (t *Tracker) GetProgress() (int, int, int64, int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	return t.currentChunk, t.totalChunks, t.bytesSent, t.totalBytes
}

// Reset resets the tracker for a new file
func (t *Tracker) Reset(filename string, totalChunks int, totalBytes int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	t.filename = filename
	t.totalChunks = totalChunks
	t.totalBytes = totalBytes
	t.currentChunk = 0
	t.bytesSent = 0
	t.startTime = time.Now()
}