package telemetry

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LogRotator handles log file rotation by size and time.
type LogRotator struct {
	mu           sync.Mutex
	file         *os.File
	dir          string
	filename     string
	maxSize      int64 // bytes
	maxAge       time.Duration
	compress     bool
	currentSize  int64
	closed       bool
}

// NewLogRotator creates a new log rotator.
func NewLogRotator(cfg Config) *LogRotator {
	if cfg.Filename == "" {
		cfg.Filename = cfg.Component + ".log"
	}

	maxSize := int64(cfg.MaxSizeMB) * 1024 * 1024
	if maxSize <= 0 {
		maxSize = 100 * 1024 * 1024 // default 100MB
	}

	maxAge := time.Duration(cfg.MaxAgeDays) * 24 * time.Hour
	if cfg.MaxAgeDays <= 0 {
		maxAge = 0 // no age-based rotation
	}

	// Ensure directory exists
	if err := os.MkdirAll(cfg.OutputPath, 0755); err != nil {
		// Fallback to stdout-like behavior
		return &LogRotator{closed: true}
	}

	filepath := filepath.Join(cfg.OutputPath, cfg.Filename)
	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return &LogRotator{closed: true}
	}

	// Get current file size
	var currentSize int64
	if info, err := file.Stat(); err == nil {
		currentSize = info.Size()
	}

	r := &LogRotator{
		file:        file,
		dir:         cfg.OutputPath,
		filename:    cfg.Filename,
		maxSize:     maxSize,
		maxAge:      maxAge,
		compress:    cfg.Compress,
		currentSize: currentSize,
	}

	// Start background cleaner for age-based retention
	if r.maxAge > 0 {
		go r.cleanupLoop()
	}

	return r
}

// Write implements io.Writer.
func (r *LogRotator) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed || r.file == nil {
		return 0, os.ErrClosed
	}

	// Check if rotation needed
	if r.maxSize > 0 && r.currentSize+int64(len(p)) > r.maxSize {
		if err := r.rotateLocked(); err != nil {
			return 0, err
		}
	}

	n, err = r.file.Write(p)
	r.currentSize += int64(n)
	return n, err
}

// rotateLocked rotates the log file (must hold lock).
func (r *LogRotator) rotateLocked() error {
	if r.file == nil {
		return nil
	}

	// Generate rotated filename with timestamp
	timestamp := time.Now().Format("20060102-150405")
	rotatedName := r.filename + "." + timestamp
	rotatedPath := filepath.Join(r.dir, rotatedName)
	currentPath := filepath.Join(r.dir, r.filename)

	// Close current file first
	if err := r.file.Close(); err != nil {
		return err
	}
	r.file = nil

	// Small delay to ensure file handle is released on Windows
	time.Sleep(10 * time.Millisecond)

	// Rename current file to rotated name
	if err := os.Rename(currentPath, rotatedPath); err != nil {
		// Try to reopen original file
		r.file, _ = os.OpenFile(currentPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		return err
	}

	// Compress if enabled (async)
	if r.compress {
		go r.compressFile(rotatedPath)
	}

	// Create new log file
	file, err := os.OpenFile(currentPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	r.file = file
	r.currentSize = 0
	return nil
}

// compressFile compresses a rotated log file.
func (r *LogRotator) compressFile(path string) {
	gzPath := path + ".gz"

	// Open source
	src, err := os.Open(path)
	if err != nil {
		return
	}
	defer src.Close()

	// Create destination
	dst, err := os.Create(gzPath)
	if err != nil {
		return
	}
	defer dst.Close()

	// Compress
	gz := gzip.NewWriter(dst)
	defer gz.Close()

	if _, err := io.Copy(gz, src); err != nil {
		os.Remove(gzPath)
		return
	}

	// Remove original
	os.Remove(path)
}

// cleanupLoop periodically removes old rotated files.
func (r *LogRotator) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		r.mu.Lock()
		closed := r.closed
		r.mu.Unlock()

		if closed {
			return
		}

		r.cleanupOldFiles()
	}
}

// cleanupOldFiles removes rotated files older than maxAge.
func (r *LogRotator) cleanupOldFiles() {
	if r.maxAge <= 0 {
		return
	}

	cutoff := time.Now().Add(-r.maxAge)

	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return
	}

	prefix := r.filename + "."
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		// Skip current log file
		if name == r.filename {
			continue
		}

		path := filepath.Join(r.dir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			os.Remove(path)
		}
	}
}

// Close closes the rotator.
func (r *LogRotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	r.closed = true
	if r.file != nil {
		err := r.file.Close()
		r.file = nil
		return err
	}
	return nil
}

// SetMaxSize sets the max size for testing purposes.
func (r *LogRotator) SetMaxSize(bytes int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxSize = bytes
}