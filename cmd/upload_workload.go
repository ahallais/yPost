package cmd

import (
	"fmt"
	"math"
	"os"
)

const defaultArticlesPerConnection = int64(8)

type uploadWorkload struct {
	Bytes    int64
	Articles int64
}

// workloadAwareConnectionLimit treats requested as a ceiling. A connection
// must have enough aggregate byte work to justify its setup, and the result is
// also capped at the greatest number of articles that any one file can keep
// busy because files are posted sequentially.
func workloadAwareConnectionLimit(requested int, articleSize, targetBytes int64, paths []string) (int, uploadWorkload, error) {
	if requested < 1 {
		requested = 1
	}
	if articleSize <= 0 {
		return 0, uploadWorkload{}, fmt.Errorf("article size must be positive")
	}
	if targetBytes == 0 {
		if articleSize > math.MaxInt64/defaultArticlesPerConnection {
			targetBytes = math.MaxInt64
		} else {
			targetBytes = defaultArticlesPerConnection * articleSize
		}
	}
	if targetBytes < 0 {
		return 0, uploadWorkload{}, fmt.Errorf("target bytes per connection cannot be negative")
	}

	var workload uploadWorkload
	var usefulCap int64
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return 0, uploadWorkload{}, fmt.Errorf("stat upload file %s: %w", path, err)
		}
		size := info.Size()
		if size > math.MaxInt64-workload.Bytes {
			return 0, uploadWorkload{}, fmt.Errorf("upload workload size overflows int64")
		}
		workload.Bytes += size
		articles := ceilDivPositive(size, articleSize)
		if articles > math.MaxInt64-workload.Articles {
			return 0, uploadWorkload{}, fmt.Errorf("upload article count overflows int64")
		}
		workload.Articles += articles
		if articles > usefulCap {
			usefulCap = articles
		}
	}

	workCap := ceilDivPositive(workload.Bytes, targetBytes)
	limit := minInt64(int64(requested), usefulCap, workCap)
	if limit < 1 {
		limit = 1
	}
	return int(limit), workload, nil
}

func ceilDivPositive(value, divisor int64) int64 {
	if value <= 0 {
		return 0
	}
	return (value-1)/divisor + 1
}

func minInt64(values ...int64) int64 {
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}
