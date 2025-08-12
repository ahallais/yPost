package yenc

import (
	"bytes"
	"crypto/crc32"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const (
	yencHeader  = "=ybegin"
	yencTrailer = "=yend"
	lineLength  = 128
)

// Encoder handles yEnc encoding
type Encoder struct {
	crc32 uint32
	size  int64
}

// Encode encodes data using yEnc format
func (e *Encoder) Encode(data []byte, filename string, partNum int, totalParts int) string {
	var buf bytes.Buffer
	
	// Calculate CRC32
	e.crc32 = crc32.ChecksumIEEE(data)
	e.size = int64(len(data))
	
	// Write header
	header := e.buildHeader(filename, partNum, totalParts)
	buf.WriteString(header)
	buf.WriteString("\r\n")
	
	// Encode data
	encoded := e.encodeData(data)
	
	// Split into lines
	lines := e.splitIntoLines(encoded)
	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteString("\r\n")
	}
	
	// Write trailer
	trailer := e.buildTrailer()
	buf.WriteString(trailer)
	buf.WriteString("\r\n")
	
	return buf.String()
}

// buildHeader creates the yEnc header
func (e *Encoder) buildHeader(filename string, partNum int, totalParts int) string {
	if totalParts > 1 {
		return fmt.Sprintf("%s part=%d total=%d line=%d size=%d name=%s",
			yencHeader, partNum, totalParts, lineLength, e.size, filename)
	}
	return fmt.Sprintf("%s line=%d size=%d name=%s",
		yencHeader, lineLength, e.size, filename)
}

// buildTrailer creates the yEnc trailer
func (e *Encoder) buildTrailer() string {
	return fmt.Sprintf("%s size=%d crc32=%s", yencTrailer, e.size, strings.ToUpper(hex.EncodeToString([]byte{byte(e.crc32 >> 24), byte(e.crc32 >> 16), byte(e.crc32 >> 8), byte(e.crc32)})))
}

// encodeData performs the actual yEnc encoding
func (e *Encoder) encodeData(data []byte) []byte {
	var result []byte
	
	for _, b := range data {
		// yEnc encoding: add 42 to each byte, escape special chars
		encoded := b + 42
		
		// Escape special characters
		switch encoded {
		case 0, 9, 10, 13, '=':
			result = append(result, '=')
			encoded += 64
		}
		
		result = append(result, encoded)
	}
	
	return result
}

// splitIntoLines splits encoded data into lines of specified length
func (e *Encoder) splitIntoLines(data []byte) []string {
	var lines []string
	
	for i := 0; i < len(data); i += lineLength {
		end := i + lineLength
		if end > len(data) {
			end = len(data)
		}
		lines = append(lines, string(data[i:end]))
	}
	
	return lines
}

// GetCRC32 returns the CRC32 checksum of the last encoded data
func (e *Encoder) GetCRC32() uint32 {
	return e.crc32
}

// GetSize returns the size of the last encoded data
func (e *Encoder) GetSize() int64 {
	return e.size
}

// Decode decodes yEnc encoded data
func Decode(encoded string) ([]byte, error) {
	lines := strings.Split(encoded, "\r\n")
	var data []byte
	
	// Find start and end of encoded data
	start := 0
	end := len(lines)
	
	for i, line := range lines {
		if strings.HasPrefix(line, yencHeader) {
			start = i + 1
		}
		if strings.HasPrefix(line, yencTrailer) {
			end = i
			break
		}
	}
	
	// Decode data
	for i := start; i < end; i++ {
		decoded, err := decodeLine(lines[i])
		if err != nil {
			return nil, err
		}
		data = append(data, decoded...)
	}
	
	return data, nil
}

// decodeLine decodes a single line of yEnc data
func decodeLine(line string) ([]byte, error) {
	var result []byte
	i := 0
	
	for i < len(line) {
		c := line[i]
		
		if c == '=' {
			// Escaped character
			if i+1 >= len(line) {
				return nil, fmt.Errorf("incomplete escape sequence")
			}
			decoded := line[i+1] - 64
			result = append(result, decoded-42)
			i += 2
		} else {
			// Normal character
			result = append(result, c-42)
			i++
		}
	}
	
	return result, nil
}

// EncoderReader wraps an io.Reader to provide yEnc encoding
type EncoderReader struct {
	reader io.Reader
	buffer bytes.Buffer
	header string
	trailer string
	done    bool
}

// NewEncoderReader creates a new yEnc encoder reader
func NewEncoderReader(reader io.Reader, filename string, partNum int, totalParts int, fileSize int64) *EncoderReader {
	encoder := &Encoder{}
	header := encoder.buildHeader(filename, partNum, totalParts)
	trailer := encoder.buildTrailer()
	
	return &EncoderReader{
		reader:  reader,
		header:  header,
		trailer: trailer,
	}
}

// Read implements io.Reader interface
func (er *EncoderReader) Read(p []byte) (n int, err error) {
	if !er.done && er.buffer.Len() == 0 {
		// Add header if not done
		if er.header != "" {
			er.buffer.WriteString(er.header)
			er.buffer.WriteString("\r\n")
			er.header = ""
		}
		
		// Read and encode data
		buf := make([]byte, 8192)
		n, err := er.reader.Read(buf)
		if err != nil && err != io.EOF {
			return 0, err
		}
		
		if n > 0 {
			encoder := &Encoder{}
			encoded := encoder.encodeData(buf[:n])
			lines := encoder.splitIntoLines(encoded)
			for _, line := range lines {
				er.buffer.WriteString(line)
				er.buffer.WriteString("\r\n")
			}
		}
		
		if err == io.EOF {
			// Add trailer
			er.buffer.WriteString(er.trailer)
			er.buffer.WriteString("\r\n")
			er.done = true
		}
	}
	
	return er.buffer.Read(p)
}