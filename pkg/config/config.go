package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Agent     AgentConfig     `mapstructure:"agent"`
	Discovery DiscoveryConfig `mapstructure:"discovery"`
	Collector CollectorConfig `mapstructure:"collector"`
	Analyzer  AnalyzerConfig  `mapstructure:"analyzer"`
	Storage   StorageConfig   `mapstructure:"storage"`
	Cleaner   CleanerConfig   `mapstructure:"cleaner"`
	Monitor   MonitorConfig   `mapstructure:"monitor"`
}

type AgentConfig struct {
	Name        string `mapstructure:"name"`
	Namespace   string `mapstructure:"namespace"`
	LogLevel    string `mapstructure:"logLevel"`
	MetricsPort int    `mapstructure:"metricsPort"`
	HealthPort  int    `mapstructure:"healthPort"`
}

type DiscoveryConfig struct {
	ScanInterval       time.Duration `mapstructure:"scanInterval"`
	Namespaces         []string      `mapstructure:"namespaces"`
	HelmReleaseLabels  []string      `mapstructure:"helmReleaseLabels"`
	OperatorLabels     []string      `mapstructure:"operatorLabels"`
}

type CollectorConfig struct {
	CoredumpPath     string        `mapstructure:"coredumpPath"`
	HostCoredumpPath string        `mapstructure:"hostCoredumpPath"`
	WatchInterval    time.Duration `mapstructure:"watchInterval"`
	MaxFileAge       time.Duration `mapstructure:"maxFileAge"`
	MaxFileSize      string        `mapstructure:"maxFileSize"`
}

type AnalyzerConfig struct {
	EnableGdbAnalysis bool          `mapstructure:"enableGdbAnalysis"`
	GdbTimeout        time.Duration `mapstructure:"gdbTimeout"`
	ValueThreshold    float64       `mapstructure:"valueThreshold"`
	IgnorePatterns    []string      `mapstructure:"ignorePatterns"`
	PanicKeywords     []string      `mapstructure:"panicKeywords"`
	AIAnalysis        AIAnalysisConfig `mapstructure:"aiAnalysis"`
}

type AIAnalysisConfig struct {
	Enabled           bool          `mapstructure:"enabled"`
	Provider          string        `mapstructure:"provider"`
	Model             string        `mapstructure:"model"`
	APIKey            string        `mapstructure:"apiKey"`
	BaseURL           string        `mapstructure:"baseURL"`
	Timeout           time.Duration `mapstructure:"timeout"`
	MaxTokens         int           `mapstructure:"maxTokens"`
	Temperature       float32       `mapstructure:"temperature"`
	EnableCostControl bool          `mapstructure:"enableCostControl"`
	MaxCostPerMonth   float64       `mapstructure:"maxCostPerMonth"`
	MaxAnalysisPerHour int          `mapstructure:"maxAnalysisPerHour"`
}

type StorageConfig struct {
	Backend           string        `mapstructure:"backend"`
	LocalPath         string        `mapstructure:"localPath"`
	MaxStorageSize    string        `mapstructure:"maxStorageSize"`
	RetentionDays     int           `mapstructure:"retentionDays"`
	CompressionEnabled bool         `mapstructure:"compressionEnabled"`
	S3                S3Config      `mapstructure:"s3"`
}

type S3Config struct {
	Bucket    string `mapstructure:"bucket"`
	Region    string `mapstructure:"region"`
	Endpoint  string `mapstructure:"endpoint"`
	AccessKey string `mapstructure:"accessKey"`
	SecretKey string `mapstructure:"secretKey"`
}

type CleanerConfig struct {
	Enabled           bool          `mapstructure:"enabled"`
	MaxRestartCount   int           `mapstructure:"maxRestartCount"`
	RestartTimeWindow time.Duration `mapstructure:"restartTimeWindow"`
	CleanupDelay      time.Duration `mapstructure:"cleanupDelay"`
	UninstallTimeout  time.Duration `mapstructure:"uninstallTimeout"`
}

type MonitorConfig struct {
	PrometheusEnabled bool          `mapstructure:"prometheusEnabled"`
	Alerting          AlertingConfig `mapstructure:"alerting"`
}

type AlertingConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	WebhookURL string `mapstructure:"webhookUrl"`
}

func Load(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

func (c *Config) Validate() error {
	if c.Agent.Name == "" {
		return fmt.Errorf("agent name cannot be empty")
	}
	
	if c.Agent.MetricsPort <= 0 || c.Agent.MetricsPort > 65535 {
		return fmt.Errorf("invalid metrics port: %d", c.Agent.MetricsPort)
	}
	
	if c.Collector.CoredumpPath == "" {
		return fmt.Errorf("coredump path cannot be empty")
	}
	
	if c.Storage.Backend != "local" && c.Storage.Backend != "s3" && c.Storage.Backend != "nfs" {
		return fmt.Errorf("unsupported storage backend: %s", c.Storage.Backend)
	}
	
	return nil
}