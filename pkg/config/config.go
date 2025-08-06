package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Agent           AgentConfig           `mapstructure:"agent"`
	Controller      ControllerConfig      `mapstructure:"controller"`
	Database        DatabaseConfig        `mapstructure:"database"`
	Discovery       DiscoveryConfig       `mapstructure:"discovery"`
	Collector       CollectorConfig       `mapstructure:"collector"`
	LogCollector    LogCollectorConfig    `mapstructure:"logCollector"`
	MetricsCollector MetricsCollectorConfig `mapstructure:"metricsCollector"`
	Analyzer        AnalyzerConfig        `mapstructure:"analyzer"`
	Storage         StorageConfig         `mapstructure:"storage"`
	Cleaner         CleanerConfig         `mapstructure:"cleaner"`
	Monitor         MonitorConfig         `mapstructure:"monitor"`
	Dashboard       DashboardConfig       `mapstructure:"dashboard"`
	Reporter        ReporterConfig        `mapstructure:"reporter"`
}

type AgentConfig struct {
	Name        string `mapstructure:"name"`
	NodeName    string `mapstructure:"nodeName"`
	Namespace   string `mapstructure:"namespace"`
	LogLevel    string `mapstructure:"logLevel"`
	MetricsPort int    `mapstructure:"metricsPort"`
	HealthPort  int    `mapstructure:"healthPort"`
}

type ControllerConfig struct {
	Enabled           bool          `mapstructure:"enabled"`
	URL               string        `mapstructure:"url"`
	Timeout           time.Duration `mapstructure:"timeout"`
	HeartbeatInterval time.Duration `mapstructure:"heartbeatInterval"`
}

type DatabaseConfig struct {
	Path            string        `mapstructure:"path"`
	MaxOpenConns    int           `mapstructure:"maxOpenConns"`
	MaxIdleConns    int           `mapstructure:"maxIdleConns"`
	ConnMaxLifetime time.Duration `mapstructure:"connMaxLifetime"`
	RetentionDays   int           `mapstructure:"retentionDays"`
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

type DashboardConfig struct {
	Enabled         bool               `mapstructure:"enabled"`
	Port            int                `mapstructure:"port"`
	Path            string             `mapstructure:"path"`
	ServeStatic     bool               `mapstructure:"serveStatic"`
	StaticPath      string             `mapstructure:"staticPath"`
	ViewerNamespace string             `mapstructure:"viewerNamespace"`
	Viewer          ViewerConfig       `mapstructure:"viewer"`
}

type ViewerConfig struct {
	Enabled           bool   `mapstructure:"enabled"`
	Image             string `mapstructure:"image"`
	ImagePullPolicy   string `mapstructure:"imagePullPolicy"`
	DefaultDuration   int    `mapstructure:"defaultDuration"`
	MaxDuration       int    `mapstructure:"maxDuration"`
	MaxConcurrentPods int    `mapstructure:"maxConcurrentPods"`
	CoredumpPath      string `mapstructure:"coredumpPath"`
	WebTerminalPort   int    `mapstructure:"webTerminalPort"`
}

type LogCollectorConfig struct {
	Enabled     bool              `mapstructure:"enabled"`
	Source      string            `mapstructure:"source"` // "loki" or "file"
	Loki        LokiConfig        `mapstructure:"loki"`
	Patterns    LogPatternConfig  `mapstructure:"patterns"`
	BufferSize  int               `mapstructure:"bufferSize"`
}

type LogPatternConfig struct {
	ErrorPatterns    []string `mapstructure:"errorPatterns"`
	WarningPatterns  []string `mapstructure:"warningPatterns"`
	ExcludePatterns  []string `mapstructure:"excludePatterns"`
	TimestampFormat  string   `mapstructure:"timestampFormat"`
	Multiline        bool     `mapstructure:"multiline"`
	MultilinePattern string   `mapstructure:"multilinePattern"`
}

type LokiConfig struct {
	URL             string          `mapstructure:"url"`
	Timeout         time.Duration   `mapstructure:"timeout"`
	BatchSize       int             `mapstructure:"batchSize"`
	QueryInterval   time.Duration   `mapstructure:"queryInterval"`
	LookbackWindow  time.Duration   `mapstructure:"lookbackWindow"`
	Queries         []LokiQuery     `mapstructure:"queries"`
}

type LokiQuery struct {
	Name   string   `mapstructure:"name"`
	Query  string   `mapstructure:"query"`
	Labels []string `mapstructure:"labels"`
}

type MetricsCollectorConfig struct {
	Enabled            bool                    `mapstructure:"enabled"`
	Source             string                  `mapstructure:"source"` // "prometheus" or "direct"
	Prometheus         PrometheusConfig        `mapstructure:"prometheus"`
	CollectionInterval time.Duration           `mapstructure:"collectionInterval"`
	RetentionPeriod    time.Duration           `mapstructure:"retentionPeriod"`
}

type PrometheusConfig struct {
	URL             string               `mapstructure:"url"`
	Timeout         time.Duration        `mapstructure:"timeout"`
	QueryInterval   time.Duration        `mapstructure:"queryInterval"`
	LookbackWindow  time.Duration        `mapstructure:"lookbackWindow"`
	Queries         []PrometheusQuery    `mapstructure:"queries"`
}

type PrometheusQuery struct {
	Name   string   `mapstructure:"name"`
	Query  string   `mapstructure:"query"`
	Labels []string `mapstructure:"labels"`
}


type ReporterConfig struct {
	Enabled           bool                `mapstructure:"enabled"`
	OutputPath        string              `mapstructure:"outputPath"`
	Schedule          ScheduleConfig      `mapstructure:"schedule"`
	Templates         TemplateConfig      `mapstructure:"templates"`
	Delivery          DeliveryConfig      `mapstructure:"delivery"`
	RetentionPeriod   time.Duration       `mapstructure:"retentionPeriod"`
	IncludeMetrics    []string            `mapstructure:"includeMetrics"`
}

type ScheduleConfig struct {
	Daily     bool   `mapstructure:"daily"`
	Weekly    bool   `mapstructure:"weekly"`
	Monthly   bool   `mapstructure:"monthly"`
	DailyAt   string `mapstructure:"dailyAt"`
	WeeklyAt  string `mapstructure:"weeklyAt"`
	MonthlyAt string `mapstructure:"monthlyAt"`
}

type TemplateConfig struct {
	DefaultTemplate string            `mapstructure:"defaultTemplate"`
	CustomTemplates map[string]string `mapstructure:"customTemplates"`
	Format          string            `mapstructure:"format"`
}

type DeliveryConfig struct {
	Email    EmailConfig     `mapstructure:"email"`
	Webhook  WebhookConfig   `mapstructure:"webhook"`
	Storage  StorageDelivery `mapstructure:"storage"`
}

type EmailConfig struct {
	Enabled    bool     `mapstructure:"enabled"`
	SMTPHost   string   `mapstructure:"smtpHost"`
	SMTPPort   int      `mapstructure:"smtpPort"`
	Username   string   `mapstructure:"username"`
	Password   string   `mapstructure:"password"`
	From       string   `mapstructure:"from"`
	Recipients []string `mapstructure:"recipients"`
	Subject    string   `mapstructure:"subject"`
}

type WebhookConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	URL     string `mapstructure:"url"`
	Method  string `mapstructure:"method"`
	Headers map[string]string `mapstructure:"headers"`
	Timeout time.Duration     `mapstructure:"timeout"`
}

type StorageDelivery struct {
	Enabled    bool   `mapstructure:"enabled"`
	Path       string `mapstructure:"path"`
	MaxReports int    `mapstructure:"maxReports"`
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
	
	// Validate LogCollector config
	if c.LogCollector.Enabled {
		if c.LogCollector.Source == "loki" {
			if c.LogCollector.Loki.URL == "" {
				return fmt.Errorf("log collector enabled with Loki source but no URL specified")
			}
			if len(c.LogCollector.Loki.Queries) == 0 {
				return fmt.Errorf("log collector enabled with Loki source but no queries specified")
			}
		}
	}
	
	// Validate MetricsCollector config
	if c.MetricsCollector.Enabled {
		if c.MetricsCollector.CollectionInterval <= 0 {
			return fmt.Errorf("metrics collector interval must be positive")
		}
	}
	
	// Validate Reporter config
	if c.Reporter.Enabled {
		if c.Reporter.OutputPath == "" {
			return fmt.Errorf("reporter enabled but no output path specified")
		}
		if !c.Reporter.Schedule.Daily && !c.Reporter.Schedule.Weekly && !c.Reporter.Schedule.Monthly {
			return fmt.Errorf("reporter enabled but no schedule configured")
		}
	}
	
	return nil
}