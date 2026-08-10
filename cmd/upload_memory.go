package cmd

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

const (
	yencWorkerMemoryMultiplier = int64(8)
	workerFixedMemoryEstimate  = int64(2 * 1024 * 1024)
)

func availableMemory() (int64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	return parseAvailableMemory(string(data))
}

func parseAvailableMemory(meminfo string) (int64, error) {
	scanner := bufio.NewScanner(strings.NewReader(meminfo))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || fields[0] != "MemAvailable:" {
			continue
		}
		kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kilobytes < 0 {
			return 0, fmt.Errorf("invalid MemAvailable value %q", fields[1])
		}
		return kilobytes * 1024, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("MemAvailable is missing from /proc/meminfo")
}

// memoryAwareConnectionLimit reserves half of currently available memory for
// the OS and other phases. The worker estimate covers the raw article buffer,
// worst-case yEnc expansion, encoder temporaries, the post body, and TLS/buffer
// overhead. At least one worker is always retained.
func memoryAwareConnectionLimit(requested int, articleSize, memoryAvailable int64) (connections int, estimatedPerWorker int64) {
	if requested < 1 {
		requested = 1
	}
	if articleSize < 0 || articleSize > (math.MaxInt64-workerFixedMemoryEstimate)/yencWorkerMemoryMultiplier {
		estimatedPerWorker = math.MaxInt64
	} else {
		estimatedPerWorker = articleSize*yencWorkerMemoryMultiplier + workerFixedMemoryEstimate
	}
	budget := memoryAvailable / 2
	limit := int64(1)
	if estimatedPerWorker > 0 && budget >= estimatedPerWorker {
		limit = budget / estimatedPerWorker
	}
	if limit > int64(requested) {
		limit = int64(requested)
	}
	return int(limit), estimatedPerWorker
}

func formatMemory(bytes int64) string {
	const mib = int64(1024 * 1024)
	if bytes < mib {
		return fmt.Sprintf("%.1f KiB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(bytes)/float64(mib))
}
