package collector

import (
	"errors"
	"testing"
)

func TestBasicCoredumpPatternMatching(t *testing.T) {
	tests := []struct {
		filename    string
		shouldMatch bool
		description string
	}{
		{
			filename:    "core.milvus.1000.1234567890.15678",
			shouldMatch: true,
			description: "Standard milvus coredump pattern",
		},
		{
			filename:    "core.milvus_crasher.1001.1634567890.15679",
			shouldMatch: true,
			description: "Milvus crasher pattern",
		},
		{
			filename:    "core.milvus.1000.a1b2c3d4.1234567890.15678",
			shouldMatch: true,
			description: "systemd coredump pattern with hex",
		},
		{
			filename:    "milvus.log",
			shouldMatch: false,
			description: "Log file should not match",
		},
		{
			filename:    "regular_file.txt",
			shouldMatch: false,
			description: "Regular file should not match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			standardMatch := coredumpPattern.MatchString(tt.filename)
			systemdMatch := systemdPattern.MatchString(tt.filename)
			matches := standardMatch || systemdMatch

			if matches != tt.shouldMatch {
				t.Errorf("filename %s: expected match=%v, got match=%v (standard=%v, systemd=%v)",
					tt.filename, tt.shouldMatch, matches, standardMatch, systemdMatch)
			}
		})
	}
}

func TestBasicCoredumpInfoExtraction(t *testing.T) {
	tests := []struct {
		filename        string
		expectedProcess string
		expectedPID     string
		expectError     bool
		description     string
	}{
		{
			filename:        "core.milvus.1000.1234567890.15678",
			expectedProcess: "milvus",
			expectedPID:     "15678",
			expectError:     false,
			description:     "Standard pattern extraction",
		},
		{
			filename:        "core.milvus_crasher.1001.1634567890.15679",
			expectedProcess: "milvus_crasher",
			expectedPID:     "15679",
			expectError:     false,
			description:     "Process with underscore",
		},
		{
			filename:        "core.milvus.1000.a1b2c3d4.1234567890.15678",
			expectedProcess: "milvus",
			expectedPID:     "15678",
			expectError:     false,
			description:     "systemd pattern extraction",
		},
		{
			filename:    "invalid_file.txt",
			expectError: true,
			description: "Invalid filename pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			process, pid, err := extractBasicCoredumpInfo(tt.filename)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if process != tt.expectedProcess {
				t.Errorf("expected process %s, got %s", tt.expectedProcess, process)
			}

			if pid != tt.expectedPID {
				t.Errorf("expected PID %s, got %s", tt.expectedPID, pid)
			}
		})
	}
}

// Helper function for basic info extraction
func extractBasicCoredumpInfo(filename string) (process, pid string, err error) {
	// Try standard pattern first
	if matches := coredumpPattern.FindStringSubmatch(filename); len(matches) >= 5 {
		return matches[1], matches[4], nil
	}
	
	// Try systemd pattern
	if matches := systemdPattern.FindStringSubmatch(filename); len(matches) >= 6 {
		return matches[1], matches[5], nil
	}
	
	return "", "", errors.New("no match")
}