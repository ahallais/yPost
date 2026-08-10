package yenc

import (
	"bufio"
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
	"sync"
)

const (
	yencHeader  = "=ybegin"
	yencTrailer = "=yend"
	lineLength  = 128
)

// Encoder handles yEnc encoding
type Encoder struct {
	crc32      uint32
	size       int64
	lineLength int
	mu         sync.Mutex
}

// NewEncoder creates an encoder with the requested yEnc line length.
func NewEncoder(maxLineLength int) *Encoder {
	if maxLineLength <= 0 {
		maxLineLength = lineLength
	}
	return &Encoder{lineLength: maxLineLength}
}

func (e *Encoder) effectiveLineLength() int {
	if e.lineLength > 0 {
		return e.lineLength
	}
	return lineLength
}

// Encode encodes data using yEnc format
func (e *Encoder) Encode(data []byte, filename string, partNum int, totalParts int) string {
	return e.EncodePart(data, filename, partNum, totalParts, int64(len(data)), 1)
}

// EncodePart encodes one article of a multipart yEnc file. fileSize is the
// size of the complete file and begin is the one-based byte offset of data.
func (e *Encoder) EncodePart(data []byte, filename string, partNum int, totalParts int, fileSize int64, begin int64) string {
	var buf bytes.Buffer
	_ = e.EncodePartTo(&buf, data, filename, partNum, totalParts, fileSize, begin)
	return buf.String()
}

// EncodePartTo streams one yEnc article to w without allocating an encoded
// copy of the complete article. The raw input remains owned by the caller and
// can be replayed when an NNTP retry is required.
func (e *Encoder) EncodePartTo(w io.Writer, data []byte, filename string, partNum int, totalParts int, fileSize int64, begin int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	writer := bufio.NewWriterSize(w, 32*1024)

	e.crc32 = crc32.ChecksumIEEE(data)
	e.size = int64(len(data))

	header := e.buildHeader(filename, partNum, totalParts, fileSize)
	if _, err := fmt.Fprintf(writer, "%s\r\n", header); err != nil {
		return err
	}
	if totalParts > 1 {
		if _, err := fmt.Fprintf(writer, "=ypart begin=%d end=%d\r\n", begin, begin+int64(len(data))-1); err != nil {
			return err
		}
	}

	linePosition := 0
	for _, value := range data {
		encoded := value + 42
		encodedLength := 1
		if encoded == 0 || encoded == 9 || encoded == 10 || encoded == 13 || encoded == '=' {
			encodedLength = 2
		}
		if linePosition+encodedLength > e.effectiveLineLength() {
			if _, err := writer.WriteString("\r\n"); err != nil {
				return err
			}
			linePosition = 0
		}
		if encodedLength == 2 {
			if err := writer.WriteByte('='); err != nil {
				return err
			}
			encoded += 64
		}
		if err := writer.WriteByte(encoded); err != nil {
			return err
		}
		linePosition += encodedLength
	}
	if linePosition > 0 {
		if _, err := writer.WriteString("\r\n"); err != nil {
			return err
		}
	}

	trailer := e.buildTrailer(partNum, totalParts > 1)
	if _, err := fmt.Fprintf(writer, "%s\r\n", trailer); err != nil {
		return err
	}
	return writer.Flush()
}

// buildHeader creates the yEnc header matching Node.js format
func (e *Encoder) buildHeader(filename string, partNum int, totalParts int, fileSize int64) string {
	if totalParts > 1 {
		return fmt.Sprintf("%s part=%d total=%d line=%d size=%d name=%s",
			yencHeader, partNum, totalParts, e.effectiveLineLength(), fileSize, filename)
	}
	return fmt.Sprintf("%s line=%d size=%d name=%s",
		yencHeader, e.effectiveLineLength(), e.size, filename)
}

// buildTrailer creates the yEnc trailer
func (e *Encoder) buildTrailer(partNum int, multipart bool) string {
	if multipart {
		return fmt.Sprintf("%s size=%d part=%d pcrc32=%08x", yencTrailer, e.size, partNum, e.crc32)
	}
	return fmt.Sprintf("%s size=%d crc32=%08x", yencTrailer, e.size, e.crc32)
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

	for i := 0; i < len(data); {
		end := i + e.effectiveLineLength()
		if end > len(data) {
			end = len(data)
		}
		// Never split an escape sequence across a line boundary.
		if end < len(data) && data[end-1] == '=' {
			end--
		}
		lines = append(lines, string(data[i:end]))
		i = end
	}

	return lines
}

// GetCRC32 returns the CRC32 checksum of the last encoded data
func (e *Encoder) GetCRC32() uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.crc32
}

// GetSize returns the size of the last encoded data
func (e *Encoder) GetSize() int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
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
		if strings.HasPrefix(lines[i], "=ypart ") {
			continue
		}
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
	reader  io.Reader
	buffer  bytes.Buffer
	header  string
	trailer string
	done    bool
}

// NewEncoderReader creates a new yEnc encoder reader
func NewEncoderReader(reader io.Reader, filename string, partNum int, totalParts int, fileSize int64) *EncoderReader {
	encoder := &Encoder{}
	header := encoder.buildHeader(filename, partNum, totalParts, fileSize)
	trailer := encoder.buildTrailer(partNum, totalParts > 1)

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
