package progress

import (
	"fmt"
	"sync"
	"time"
)

// StageTracker prints periodic, single-line snapshots for a long-running stage.
// It is deliberately independent of the per-file upload progress bars.
type StageTracker struct {
	mu          sync.Mutex
	name        string
	total       int64
	completed   int64
	started     time.Time
	lastPrinted time.Time
	interval    time.Duration
	complete    bool
	bytes       bool
}

func NewStageTracker(name string, total int64) *StageTracker {
	return &StageTracker{
		name:     name,
		total:    total,
		started:  time.Now(),
		interval: 2 * time.Second,
		bytes:    true,
	}
}

// NewCounterTracker tracks discrete units such as PAR2 recovery blocks.
func NewCounterTracker(name string, total int64) *StageTracker {
	tracker := NewStageTracker(name, total)
	tracker.bytes = false
	return tracker
}

func (t *StageTracker) Add(delta int64) {
	if delta <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.completed += delta
	if t.total > 0 && t.completed > t.total {
		t.completed = t.total
	}
	if time.Since(t.lastPrinted) >= t.interval || (t.total > 0 && t.completed == t.total) {
		t.print(false)
		t.lastPrinted = time.Now()
		if t.total > 0 && t.completed == t.total {
			t.complete = true
		}
	}
}

func (t *StageTracker) Finish() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.complete {
		return
	}
	if t.total > 0 {
		t.completed = t.total
	}
	t.print(true)
	t.complete = true
}

func (t *StageTracker) print(final bool) {
	elapsed := time.Since(t.started)
	percent := float64(0)
	if t.total > 0 {
		percent = float64(t.completed) * 100 / float64(t.total)
	}
	eta := "estimating"
	if t.completed > 0 && t.total > t.completed {
		remaining := time.Duration(float64(elapsed) * float64(t.total-t.completed) / float64(t.completed))
		eta = formatDuration(remaining)
	} else if final || (t.total > 0 && t.completed == t.total) {
		eta = "00:00:00"
	}
	completed, total := fmt.Sprintf("%d", t.completed), fmt.Sprintf("%d", t.total)
	rate := ""
	if t.bytes {
		completed, total = formatAmount(t.completed), formatAmount(t.total)
		if elapsed > 0 && t.completed > 0 {
			bytesPerSecond := int64(float64(t.completed) / elapsed.Seconds())
			rate = fmt.Sprintf(", average %s/s", formatAmount(bytesPerSecond))
		}
	}
	fmt.Printf("%s overall: %5.1f%% (%s/%s) elapsed %s%s, ETA %s\n",
		t.name, percent, completed, total, formatDuration(elapsed), rate, eta)
}

func formatAmount(value int64) string {
	const unit = int64(1024)
	if value < unit {
		return fmt.Sprintf("%d", value)
	}
	div, exp := unit, 0
	for n := value / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int64(d.Round(time.Second) / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, (seconds%3600)/60, seconds%60)
}
