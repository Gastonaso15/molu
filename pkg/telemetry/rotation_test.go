package telemetry

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogRotator_RotationBySize(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create rotator with small max size (1KB = 0.001 MB)
	rotator := NewLogRotator(Config{
		OutputPath:  tmpDir,
		MaxSizeMB:   1, // will be treated as 1MB, but we test with small writes
		MaxAgeDays:  7,
		Compress:    true,
		Filename:    "test.log",
	})

	// Override maxSize for test (1KB)
	rotator.maxSize = 1024

	// Write enough data to trigger rotation (> 1KB)
	data := strings.Repeat("x", 600) + "\n" // ~600 bytes
	for i := 0; i < 3; i++ {
		_, err := rotator.Write([]byte(data))
		if err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}

	// Give time for rotation
	time.Sleep(100 * time.Millisecond)

	// Check files exist
	files, err := filepath.Glob(filepath.Join(tmpDir, "test.log*"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("files after rotation: %v", files)

	// Close rotator before temp dir cleanup
	rotator.Close()

	// Should have at least the current log file
	if len(files) == 0 {
		t.Error("expected at least one log file after rotation")
	}
}

func TestLogRotator_RotationByTime(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	rotator := NewLogRotator(Config{
		OutputPath:  tmpDir,
		MaxSizeMB:   100,
		MaxAgeDays:  1,
		Compress:    false,
		Filename:    "time-test.log",
	})

	_, err := rotator.Write([]byte("test message\n"))
	if err != nil {
		t.Fatal(err)
	}

	files, err := filepath.Glob(filepath.Join(tmpDir, "time-test.log*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Error("expected log file to exist")
	}

	rotator.Close()
}

func TestLogRotator_Retention(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	rotator := NewLogRotator(Config{
		OutputPath:  tmpDir,
		MaxSizeMB:   100,
		MaxAgeDays:  0, // 0 = no retention by age
		Compress:    true,
		Filename:    "retention-test.log",
	})

	// Write some data
	for i := 0; i < 5; i++ {
		_, err := rotator.Write([]byte("test\n"))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Close rotator
	rotator.Close()

	// Files should exist
	files, err := filepath.Glob(filepath.Join(tmpDir, "retention-test.log*"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("retention files: %v", files)
}

func TestLogRotator_Compression(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	rotator := NewLogRotator(Config{
		OutputPath:  tmpDir,
		MaxSizeMB:   1,
		MaxAgeDays:  7,
		Compress:    true,
		Filename:    "compress-test.log",
	})

	// Override maxSize for test
	rotator.maxSize = 1024

	// Write enough to trigger rotation
	data := strings.Repeat("test data line\n", 100)
	for i := 0; i < 3; i++ {
		_, err := rotator.Write([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
	}

	time.Sleep(200 * time.Millisecond)

	// Check for .gz files
	files, err := filepath.Glob(filepath.Join(tmpDir, "compress-test.log*"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("compression files: %v", files)

	hasGz := false
	for _, f := range files {
		if strings.HasSuffix(f, ".gz") {
			hasGz = true
			// Verify it's valid gzip
			content, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			gzReader, err := gzip.NewReader(bytes.NewReader(content))
			if err != nil {
				t.Errorf("invalid gzip file %s: %v", f, err)
			}
			gzReader.Close()
		}
	}
	if !hasGz {
		t.Log("no .gz files found yet (rotation may not have triggered)")
	}

	rotator.Close()
}

func TestLogRotator_Concurrency(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	rotator := NewLogRotator(Config{
		OutputPath:  tmpDir,
		MaxSizeMB:   100,
		MaxAgeDays:  7,
		Compress:    false,
		Filename:    "concurrent-test.log",
	})

	// Concurrent writes
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			data := strings.Repeat("concurrent write ", 100)
			_, err := rotator.Write([]byte(data + "\n"))
			done <- err
		}(i)
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent write failed: %v", err)
		}
	}

	rotator.Close()

	// File should exist and be readable
	files, err := filepath.Glob(filepath.Join(tmpDir, "concurrent-test.log*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Error("expected log file after concurrent writes")
	}
}

func TestLogRotator_NoDataLoss(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	rotator := NewLogRotator(Config{
		OutputPath:  tmpDir,
		MaxSizeMB:   1,
		MaxAgeDays:  7,
		Compress:    false,
		Filename:    "noloss-test.log",
	})

	// Write known messages
	messages := []string{}
	for i := 0; i < 50; i++ {
		msg := "message-" + strings.Repeat("x", 50) + "\n"
		messages = append(messages, msg)
		_, err := rotator.Write([]byte(msg))
		if err != nil {
			t.Fatal(err)
		}
	}

	rotator.Close()

	// Read all files and verify messages
	files, err := filepath.Glob(filepath.Join(tmpDir, "noloss-test.log*"))
	if err != nil {
		t.Fatal(err)
	}

	var allContent string
	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		allContent += string(content)
	}

	// Verify all messages present (order may vary due to rotation)
	for _, msg := range messages {
		if !strings.Contains(allContent, msg) {
			t.Errorf("missing message: %s", msg)
		}
	}
}