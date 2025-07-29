package analyzer

import (
	"strings"
	"testing"
	"time"

	"milvus-coredump-agent/pkg/collector"
	"milvus-coredump-agent/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBasicValueScoring(t *testing.T) {
	config := &config.AnalyzerConfig{
		ValueThreshold: 4.0,
		PanicKeywords:  []string{"panic", "fatal", "sigsegv", "sigabrt", "assert"},
	}
	
	analyzer := &Analyzer{config: config}
	
	// Create test coredump file
	coredump := &collector.CoredumpFile{
		Path:          "/test/core.milvus.1000.123.456",
		Size:          200 * 1024 * 1024, // 200MB
		ModTime:       time.Now().Add(-30 * time.Minute),
		Signal:        11, // SIGSEGV
		PodName:       "test-pod",
		ContainerName: "milvus",
		InstanceName:  "test-instance",
		CreatedAt:     metav1.Now(),
		UpdatedAt:     metav1.Now(),
	}
	
	// Create analysis results with good indicators
	results := &collector.AnalysisResults{
		CrashReason:     "Segmentation fault (SIGSEGV)",
		StackTrace:      strings.Repeat("stack trace line\n", 20), // >100 chars
		ThreadCount:     4,                                        // multiple threads
		CrashAddress:    "0x401234",
		RegisterInfo:    map[string]string{"rip": "0x401234", "rsp": "0x7fffffffe360"},
		SharedLibraries: []string{"/lib/x86_64-linux-gnu/libc.so.6"},
		LibraryVersions: map[string]string{"libc": "2.31"},
		MemoryInfo: collector.MemoryInfo{
			HeapSize:  1024 * 1024,
			StackSize: 8 * 1024,
		},
	}
	
	score := analyzer.calculateValueScore(coredump, results)
	
	// Should get high score due to:
	// - Base score: 4.0
	// - Clear crash reason: +2.0
	// - Panic keyword (sigsegv): +1.0
	// - Good stack trace: +1.5
	// - Multiple threads: +0.5
	// - Pod association: +1.0
	// - Severe signal: +1.0
	// - Large file: +0.5
	// - Fresh file: +0.5
	// Total expected: ~11.0, capped at 10.0
	
	if score < 9.0 || score > 10.0 {
		t.Errorf("Expected high value score (9.0-10.0), got %.2f", score)
	}
}

func TestBasicCrashReasonExtraction(t *testing.T) {
	analyzer := &Analyzer{}
	
	tests := []struct {
		backtrace string
		expected  string
	}{
		{
			backtrace: "Program received signal SIGSEGV, Segmentation fault.",
			expected:  "Segmentation fault (SIGSEGV)",
		},
		{
			backtrace: "Program received signal SIGABRT, Aborted.",
			expected:  "Abort signal (SIGABRT)",
		},
		{
			backtrace: "assert failed: ptr != NULL",
			expected:  "Assertion failure",
		},
		{
			backtrace: "Normal backtrace without signals",
			expected:  "Unknown crash reason",
		},
	}
	
	for _, tt := range tests {
		result := analyzer.extractCrashReason(tt.backtrace)
		if result != tt.expected {
			t.Errorf("backtrace %q: expected %q, got %q", tt.backtrace, tt.expected, result)
		}
	}
}

func TestSignalInference(t *testing.T) {
	analyzer := &Analyzer{}
	
	tests := []struct {
		signal   int
		expected string
	}{
		{11, "Segmentation fault (SIGSEGV)"},
		{6, "Abort signal (SIGABRT)"},
		{8, "Floating point exception (SIGFPE)"},
		{99, "Signal 99"},
	}
	
	for _, tt := range tests {
		result := analyzer.inferCrashReasonFromSignal(tt.signal)
		if result != tt.expected {
			t.Errorf("signal %d: expected %q, got %q", tt.signal, tt.expected, result)
		}
	}
}

func TestSkipAnalysisLogic(t *testing.T) {
	config := &config.AnalyzerConfig{
		IgnorePatterns: []string{"test", "debug"},
	}
	analyzer := &Analyzer{config: config}
	
	tests := []struct {
		name        string
		coredump    *collector.CoredumpFile
		shouldSkip  bool
		description string
	}{
		{
			name: "ignore_pattern_match",
			coredump: &collector.CoredumpFile{
				ContainerName: "test-container",
				Size:          1024 * 1024,
				ModTime:       time.Now().Add(-time.Hour),
			},
			shouldSkip:  true,
			description: "Should skip files matching ignore patterns",
		},
		{
			name: "file_too_large",
			coredump: &collector.CoredumpFile{
				ContainerName: "milvus",
				Size:          3 * 1024 * 1024 * 1024, // 3GB
				ModTime:       time.Now().Add(-time.Hour),
			},
			shouldSkip:  true,
			description: "Should skip files larger than 2GB",
		},
		{
			name: "valid_file",
			coredump: &collector.CoredumpFile{
				ContainerName: "milvus",
				Size:          100 * 1024 * 1024,
				ModTime:       time.Now().Add(-2 * time.Hour),
			},
			shouldSkip:  false,
			description: "Should not skip valid files",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.shouldSkipAnalysis(tt.coredump)
			if result != tt.shouldSkip {
				t.Errorf("%s: expected shouldSkip=%v, got %v", tt.description, tt.shouldSkip, result)
			}
		})
	}
}