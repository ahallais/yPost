package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogLevel represents the logging level
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

// Logger provides thread-safe logging functionality
type Logger struct {
	debugLogger *log.Logger
	infoLogger  *log.Logger
	warnLogger  *log.Logger
	errorLogger *log.Logger
	fatalLogger *log.Logger
	logFile     *os.File
	mu          sync.Mutex
	level       LogLevel
}

// New creates a new logger instance
func New(logDir string) (*Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	logFileName := filepath.Join(logDir, fmt.Sprintf("usenet-poster-%s.log", time.Now().Format("2006-01-02")))
	
	logFile, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Create multi-writer for both file and stdout
	multiWriter := io.MultiWriter(os.Stdout, logFile)

	logger := &Logger{
		debugLogger: log.New(multiWriter, "DEBUG: ", log.Ldate|log.Ltime|log.Lshortfile),
		infoLogger:  log.New(multiWriter, "INFO:  ", log.Ldate|log.Ltime|log.Lshortfile),
		warnLogger:  log.New(multiWriter, "WARN:  ", log.Ldate|log.Ltime|log.Lshortfile),
		errorLogger: log.New(multiWriter, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile),
		fatalLogger: log.New(multiWriter, "FATAL: ", log.Ldate|log.Ltime|log.Lshortfile),
		logFile:     logFile,
		level:       INFO,
	}

	return logger, nil
}

// SetLevel sets the logging level
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Debug logs debug messages
func (l *Logger) Debug(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.level <= DEBUG {
		l.debugLogger.Printf(format, args...)
	}
}

// Info logs informational messages
func (l *Logger) Info(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.level <= INFO {
		l.infoLogger.Printf(format, args...)
	}
}

// Warn logs warning messages
func (l *Logger) Warn(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.level <= WARN {
		l.warnLogger.Printf(format, args...)
	}
}

// Error logs error messages
func (l *Logger) Error(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.level <= ERROR {
		l.errorLogger.Printf(format, args...)
	}
}

// Fatal logs fatal messages and exits
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fatalLogger.Printf(format, args...)
	os.Exit(1)
}

// Close closes the log file
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}

// LogPostingResult logs the result of a posting operation
func (l *Logger) LogPostingResult(fileName string, totalParts int, duration time.Duration, success bool, err error) {
	if success {
		l.Info("Successfully posted %s (%d parts) in %v", fileName, totalParts, duration)
	} else {
		l.Error("Failed to post %s: %v", fileName, err)
	}
}

// LogFileSplit logs file splitting information
func (l *Logger) LogFileSplit(fileName string, totalParts int, totalSize int64) {
	l.Info("Split %s (%d bytes) into %d parts", fileName, totalSize, totalParts)
}

// LogUploadProgress logs upload progress
func (l *Logger) LogUploadProgress(fileName string, partNumber int, totalParts int, bytesUploaded int64) {
	l.Debug("Uploading %s: part %d/%d (%d bytes)", fileName, partNumber, totalParts, bytesUploaded)
}

// LogConnection logs connection information
func (l *Logger) LogConnection(server string, success bool) {
	if success {
		l.Info("Connected to server: %s", server)
	} else {
		l.Error("Failed to connect to server: %s", server)
	}
}

// LogNZBCreation logs NZB file creation
func (l *Logger) LogNZBCreation(fileName string, nzbPath string) {
	l.Info("Created NZB file: %s", nzbPath)
}

// LogPAR2Creation logs PAR2 file creation
func (l *Logger) LogPAR2Creation(fileName string, par2Files []string) {
	l.Info("Created PAR2 files: %v", par2Files)
}

// LogSFVCreation logs SFV file creation
func (l *Logger) LogSFVCreation(fileName string, sfvPath string) {
	l.Info("Created SFV file: %s", sfvPath)
}

// HistoryLogger handles posting history logging
type HistoryLogger struct {
	logger *Logger
}

// NewHistoryLogger creates a new history logger
func NewHistoryLogger(logger *Logger) *HistoryLogger {
	return &HistoryLogger{
		logger: logger,
	}
}

// LogPosting logs a posting operation to history
func (h *HistoryLogger) LogPosting(fileName string, fileSize int64, totalParts int, nzbPath string, success bool) {
	h.logger.Info("HISTORY: %s (%d bytes, %d parts) -> %s [success: %v]", 
		fileName, fileSize, totalParts, nzbPath, success)
}

// LogError logs an error to history
func (h *HistoryLogger) LogError(fileName string, err error) {
	h.logger.Error("HISTORY ERROR: %s - %v", fileName, err)
}