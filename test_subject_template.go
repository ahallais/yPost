package main

import (
	"bytes"
	"fmt"
	"text/template"
)

// TestSubjectTemplate demonstrates the fixed subject template processing
func TestSubjectTemplate() {
	// Test data
	testCases := []struct {
		name        string
		template    string
		partNumber  int
		totalParts  int
		filename    string
		fileSize    int64
		expected    string
	}{
		{
			name:       "Default template",
			template:   "",
			partNumber: 1,
			totalParts: 3,
			filename:   "Archive.rar",
			fileSize:   15938355, // 15.2MB
			expected:   "[01/03] - Archive.rar - (15.2MB) yEnc (1/3)",
		},
		{
			name:       "Custom template with placeholders",
			template:   "[{{printf \"%02d\" .Index}}/{{printf \"%02d\" .Total}}] - {{.Filename}} - ({{.Size}})",
			partNumber: 2,
			totalParts: 5,
			filename:   "Movie.mkv",
			fileSize:   1073741824, // 1GB
			expected:   "[02/05] - Movie.mkv - (1.0GB)",
		},
		{
			name:       "Config template from yaml",
			template:   "[{{.Index}}/{{.Total}}] - {{.Filename}} - ({{.Size}})",
			partNumber: 3,
			totalParts: 10,
			filename:   "Document.pdf",
			fileSize:   524288, // 512KB
			expected:   "[3/10] - Document.pdf - (512.0KB)",
		},
	}

	for _, tc := range testCases {
		fmt.Printf("Testing: %s\n", tc.name)
		
		// Calculate file size in human-readable format
		fileSize := float64(tc.fileSize)
		sizeStr := ""
		if fileSize >= 1024*1024*1024 {
			sizeStr = fmt.Sprintf("%.1fGB", fileSize/(1024*1024*1024))
		} else if fileSize >= 1024*1024 {
			sizeStr = fmt.Sprintf("%.1fMB", fileSize/(1024*1024))
		} else if fileSize >= 1024 {
			sizeStr = fmt.Sprintf("%.1fKB", fileSize/1024)
		} else {
			sizeStr = fmt.Sprintf("%dB", int(fileSize))
		}

		// Create template data
		templateData := struct {
			Index    int
			Total    int
			Filename string
			Size     string
		}{
			Index:    tc.partNumber,
			Total:    tc.totalParts,
			Filename: tc.filename,
			Size:     sizeStr,
		}

		// Process template
		subject := tc.template
		if subject == "" {
			subject = "[{{printf \"%02d\" .Index}}/{{printf \"%02d\" .Total}}] - {{.Filename}} - ({{.Size}}) yEnc ({{.Index}}/{{.Total}})"
		}

		tmpl, err := template.New("subject").Parse(subject)
		if err != nil {
			fmt.Printf("  Template parsing error: %v\n", err)
			continue
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, templateData); err != nil {
			fmt.Printf("  Template execution error: %v\n", err)
			continue
		}

		result := buf.String()
		fmt.Printf("  Result: %s\n", result)
		fmt.Printf("  Expected: %s\n", tc.expected)
		fmt.Printf("  Match: %t\n\n", result == tc.expected)
	}
}

func main() {
	fmt.Println("Testing NZB Subject Template Processing")
	fmt.Println("=======================================")
	TestSubjectTemplate()
}