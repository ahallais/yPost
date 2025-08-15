package progress

import (
	"fmt"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
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
	progressBar  *progressbar.ProgressBar
}

// NewTracker creates a new progress tracker
func NewTracker(filename string, totalChunks int, totalBytes int64) *Tracker {
	// Create a progress bar with appropriate settings
	bar := progressbar.NewOptions64(
		totalBytes,
		progressbar.OptionSetDescription(fmt.Sprintf("Uploading %s", filename)),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(50),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			fmt.Printf("\n")
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetRenderBlankState(true),
	)

	return &Tracker{
		filename:    filename,
		totalChunks: totalChunks,
		totalBytes:  totalBytes,
		startTime:   time.Now(),
		progressBar: bar,
	}
}

// EmitProgress emits progress by incrementing the progress bar
func (t *Tracker) EmitProgress(chunkNum int, bytes int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	t.currentChunk = chunkNum
	t.bytesSent += bytes
	
	// Update the progress bar with the actual bytes sent
	t.progressBar.Add64(bytes)
}

// EmitComplete emits the final progress and marks completion
func (t *Tracker) EmitComplete() {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	// Ensure progress bar is complete
	t.progressBar.Finish()
	
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
	
	// Finish current progress bar if it exists
	if t.progressBar != nil {
		t.progressBar.Finish()
	}
	
	t.filename = filename
	t.totalChunks = totalChunks
	t.totalBytes = totalBytes
	t.currentChunk = 0
	t.bytesSent = 0
	t.startTime = time.Now()
	
	// Create new progress bar for the new file
	t.progressBar = progressbar.NewOptions64(
		totalBytes,
		progressbar.OptionSetDescription(fmt.Sprintf("Uploading %s", filename)),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(50),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			fmt.Printf("\n")
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetRenderBlankState(true),
	)
}