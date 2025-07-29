package testutil

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

)

// TestFileInfo represents a test file to create
type TestFileInfo struct {
	Path     string
	Content  []byte
	Size     int64
	ModTime  time.Time
	Signal   int
}

// SetupTempDir creates a temporary directory for testing
func SetupTempDir(t *testing.T, prefix string) (string, func()) {
	tmpDir, err := ioutil.TempDir("", prefix)
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	
	cleanup := func() {
		os.RemoveAll(tmpDir)
	}
	
	return tmpDir, cleanup
}

// CreateTestFiles creates test files in the specified directory
func CreateTestFiles(t *testing.T, dir string, files []TestFileInfo) {
	for _, file := range files {
		fullPath := filepath.Join(dir, file.Path)
		
		// Create directory if needed
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", fullPath, err)
		}
		
		// Create file with content
		content := file.Content
		if content == nil {
			content = make([]byte, file.Size)
		}
		
		if err := ioutil.WriteFile(fullPath, content, 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", fullPath, err)
		}
		
		// Set modification time if specified
		if !file.ModTime.IsZero() {
			if err := os.Chtimes(fullPath, file.ModTime, file.ModTime); err != nil {
				t.Fatalf("Failed to set mod time for %s: %v", fullPath, err)
			}
		}
	}
}


// LoadTestGDBOutput loads test GDB output from testdata
func LoadTestGDBOutput(t *testing.T, filename string) string {
	data, err := ioutil.ReadFile(filepath.Join("../../testdata/gdb_outputs", filename))
	if err != nil {
		t.Fatalf("Failed to load test GDB output %s: %v", filename, err)
	}
	return string(data)
}

// AssertEventReceived asserts that an event is received on a channel within timeout
func AssertEventReceived[T any](t *testing.T, ch <-chan T, timeout time.Duration, msgAndArgs ...interface{}) T {
	select {
	case event := <-ch:
		return event
	case <-time.After(timeout):
		t.Fatalf("Expected event not received within %v: %v", timeout, msgAndArgs)
		return *new(T)
	}
}

// AssertNoEventReceived asserts that no event is received on a channel within timeout
func AssertNoEventReceived[T any](t *testing.T, ch <-chan T, timeout time.Duration, msgAndArgs ...interface{}) {
	select {
	case event := <-ch:
		t.Fatalf("Unexpected event received: %+v, %v", event, msgAndArgs)
	case <-time.After(timeout):
		// Expected - no event received
	}
}