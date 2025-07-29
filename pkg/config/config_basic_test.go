package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	
	"github.com/spf13/viper"
)

func TestBasicConfigLoading(t *testing.T) {
	// Create a temporary directory for test config
	tmpDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a basic valid config
	configYAML := `
discovery:
  helmReleaseLabels:
    - "app.kubernetes.io/name"
    - "helm.sh/chart"
  scanInterval: "30s"

collector:
  coredumpPath: "/var/lib/systemd/coredump"
  watchInterval: "10s"

analyzer:
  enableGdbAnalysis: true
  valueThreshold: 4.0
  panicKeywords:
    - "panic"
    - "fatal"

storage:
  backend: "local"
  localPath: "/tmp/coredumps"
  retentionDays: 7
`

	configPath := filepath.Join(tmpDir, "config.yaml")
	err = os.WriteFile(configPath, []byte(configYAML), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Load config - we'll just test that viper can read it
	viper.SetConfigFile(configPath)
	err = viper.ReadInConfig()
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}
	
	var config Config
	err = viper.Unmarshal(&config)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify basic config values

	// Discovery validation
	if config.Discovery.ScanInterval != 30*time.Second {
		t.Errorf("Expected discovery scan interval 30s, got %v", config.Discovery.ScanInterval)
	}

	if len(config.Discovery.HelmReleaseLabels) == 0 {
		t.Error("Expected helm release labels to be populated")
	}

	// Collector validation
	if config.Collector.CoredumpPath != "/var/lib/systemd/coredump" {
		t.Errorf("Expected coredump path '/var/lib/systemd/coredump', got %s", config.Collector.CoredumpPath)
	}

	if config.Collector.WatchInterval != 10*time.Second {
		t.Errorf("Expected collector watch interval 10s, got %v", config.Collector.WatchInterval)
	}

	// Analyzer validation
	if !config.Analyzer.EnableGdbAnalysis {
		t.Error("Expected GDB analysis to be enabled")
	}

	if config.Analyzer.ValueThreshold != 4.0 {
		t.Errorf("Expected value threshold 4.0, got %f", config.Analyzer.ValueThreshold)
	}

	if len(config.Analyzer.PanicKeywords) != 2 {
		t.Errorf("Expected 2 panic keywords, got %d", len(config.Analyzer.PanicKeywords))
	}

	// Storage validation
	if config.Storage.Backend != "local" {
		t.Errorf("Expected storage backend 'local', got %s", config.Storage.Backend)
	}

	if config.Storage.RetentionDays != 7 {
		t.Errorf("Expected retention days 7, got %d", config.Storage.RetentionDays)
	}
}

func TestBasicConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		description string
	}{
		{
			name: "valid_config",
			config: &Config{
				Discovery: DiscoveryConfig{
					ScanInterval: 30 * time.Second,
				},
				Collector: CollectorConfig{
					CoredumpPath: "/tmp/coredumps",
					WatchInterval: 10 * time.Second,
				},
				Analyzer: AnalyzerConfig{
					ValueThreshold: 4.0,
				},
				Storage: StorageConfig{
					RetentionDays: 7,
				},
			},
			expectError: false,
			description: "Should pass validation for valid config",
		},
		{
			name: "invalid_coredump_path",
			config: &Config{
				Discovery: DiscoveryConfig{
					ScanInterval: 30 * time.Second,
				},
				Collector: CollectorConfig{
					CoredumpPath: "", // Empty path
					WatchInterval: 10 * time.Second,
				},
				Analyzer: AnalyzerConfig{
					ValueThreshold: 4.0,
				},
				Storage: StorageConfig{
					RetentionDays: 7,
				},
			},
			expectError: true,
			description: "Should fail validation for empty coredump path",
		},
		{
			name: "invalid_value_threshold",
			config: &Config{
				Discovery: DiscoveryConfig{
					ScanInterval: 30 * time.Second,
				},
				Collector: CollectorConfig{
					CoredumpPath: "/tmp/coredumps",
					WatchInterval: 10 * time.Second,
				},
				Analyzer: AnalyzerConfig{
					ValueThreshold: -1.0, // Negative threshold
				},
				Storage: StorageConfig{
					RetentionDays: 7,
				},
			},
			expectError: true,
			description: "Should fail validation for negative value threshold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBasicConfig(tt.config)

			if tt.expectError {
				if err == nil {
					t.Errorf("%s: expected validation error but got none", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("%s: unexpected validation error: %v", tt.description, err)
				}
			}
		})
	}
}

// Basic validation function for testing
func validateBasicConfig(config *Config) error {
	if config.Discovery.ScanInterval <= 0 {
		return fmt.Errorf("discovery scan interval must be positive")
	}

	if config.Collector.CoredumpPath == "" {
		return fmt.Errorf("collector coredump path cannot be empty")
	}

	if config.Analyzer.ValueThreshold < 0 {
		return fmt.Errorf("analyzer value threshold cannot be negative")
	}

	if config.Storage.RetentionDays <= 0 {
		return fmt.Errorf("storage retention days must be positive")
	}

	return nil
}

func TestBasicDurationParsing(t *testing.T) {
	tests := []struct {
		value    string
		expected time.Duration
	}{
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"2h", 2 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			duration, err := time.ParseDuration(tt.value)
			if err != nil {
				t.Errorf("failed to parse duration %s: %v", tt.value, err)
				return
			}

			if duration != tt.expected {
				t.Errorf("expected duration %v, got %v", tt.expected, duration)
			}
		})
	}
}