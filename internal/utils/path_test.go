package utils

import (
	"testing"
)

func TestParseFileSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"50MB", 50 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"750KB", 750 * 1024, false},
		{"1.5GB", int64(1.5 * 1024 * 1024 * 1024), false},
		{"100", 100, false},
		{"100B", 100, false},
		{"", 0, true},
		{"invalid", 0, true},
		{"50XB", 0, true},
	}

	for _, test := range tests {
		result, err := ParseFileSize(test.input)
		
		if test.hasError {
			if err == nil {
				t.Errorf("Expected error for input %q, but got none", test.input)
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error for input %q: %v", test.input, err)
			}
			if result != test.expected {
				t.Errorf("For input %q, expected %d, got %d", test.input, test.expected, result)
			}
		}
	}
}