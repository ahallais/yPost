package progress

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
)

// Tracker handles real-time progress tracking for file transmission
type Tracker struct {
	mu            sync.Mutex
	totalChunks   int
	currentChunk  int
	filename      string
	totalBytes    int64
	bytesSent     int64
	startTime     time.Time
	progressBar   *progressbar.ProgressBar
	animationStop chan struct{}
	animationDone sync.WaitGroup
}

var activityFrames = []rune{'⠁', '⠃', '⠇', '⠏', '⠟', '⠿', '⡿', '⣿', '⡿', '⠿', '⠟', '⠏', '⠇', '⠃'}

// animatedBarWriter places an activity frame in the first unfilled bar cell.
// It returns the original byte count to satisfy io.Writer even though a
// single-byte space is replaced by a multi-byte UTF-8 rune.
type animatedBarWriter struct {
	mu    sync.Mutex
	out   io.Writer
	frame int
}

func (w *animatedBarWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	runes := []rune(string(p))
	barStart := -1
	for i, r := range runes {
		if r == '|' {
			if barStart < 0 {
				barStart = i
				continue
			}
			break
		}
		if barStart >= 0 && r == ' ' {
			runes[i] = activityFrames[w.frame%len(activityFrames)]
			w.frame++
			break
		}
	}

	_, err := io.WriteString(w.out, string(runes))
	return len(p), err
}

// NewTracker creates a new progress tracker
func NewTracker(filename string, totalChunks int, totalBytes int64) *Tracker {
	writer := &animatedBarWriter{out: os.Stdout}
	// Create a progress bar with appropriate settings
	bar := progressbar.NewOptions64(
		totalBytes,
		progressbar.OptionSetWriter(writer),
		progressbar.OptionSetDescription(fmt.Sprintf("Uploading %s", filename)),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(50),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			fmt.Printf("\n")
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionSetRenderBlankState(true),
	)

	tracker := &Tracker{
		filename:    filename,
		totalChunks: totalChunks,
		totalBytes:  totalBytes,
		startTime:   time.Now(),
		progressBar: bar,
	}
	tracker.startAnimation()
	return tracker
}

func (t *Tracker) startAnimation() {
	t.animationStop = make(chan struct{})
	t.animationDone.Add(1)
	go func() {
		defer t.animationDone.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = t.progressBar.Add64(0)
			case <-t.animationStop:
				return
			}
		}
	}()
}

func (t *Tracker) stopAnimation() {
	if t.animationStop == nil {
		return
	}
	close(t.animationStop)
	t.animationDone.Wait()
	t.animationStop = nil
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

	t.stopAnimation()
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

	t.stopAnimation()
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
	writer := &animatedBarWriter{out: os.Stdout}
	t.progressBar = progressbar.NewOptions64(
		totalBytes,
		progressbar.OptionSetWriter(writer),
		progressbar.OptionSetDescription(fmt.Sprintf("Uploading %s", filename)),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(50),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			fmt.Printf("\n")
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionSetRenderBlankState(true),
	)
	t.startAnimation()
}
