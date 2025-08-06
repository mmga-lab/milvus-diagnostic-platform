package logcollector

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"milvus-diagnostic-platform/pkg/config"
)

// LogParser handles parsing of log files and lines
type LogParser struct {
	config       *config.LogCollectorConfig
	errorRegexps []*regexp.Regexp
	warnRegexps  []*regexp.Regexp
	excludeRegexps []*regexp.Regexp
	timestampRegexp *regexp.Regexp
	multilineRegexp *regexp.Regexp
}

// NewLogParser creates a new log parser with the given configuration
func NewLogParser(config *config.LogCollectorConfig) *LogParser {
	parser := &LogParser{
		config: config,
	}
	
	// Compile error patterns
	for _, pattern := range config.Patterns.ErrorPatterns {
		if regex, err := regexp.Compile("(?i)" + pattern); err == nil {
			parser.errorRegexps = append(parser.errorRegexps, regex)
		} else {
			klog.Errorf("Failed to compile error pattern %s: %v", pattern, err)
		}
	}
	
	// Compile warning patterns
	for _, pattern := range config.Patterns.WarningPatterns {
		if regex, err := regexp.Compile("(?i)" + pattern); err == nil {
			parser.warnRegexps = append(parser.warnRegexps, regex)
		} else {
			klog.Errorf("Failed to compile warning pattern %s: %v", pattern, err)
		}
	}
	
	// Compile exclude patterns
	for _, pattern := range config.Patterns.ExcludePatterns {
		if regex, err := regexp.Compile("(?i)" + pattern); err == nil {
			parser.excludeRegexps = append(parser.excludeRegexps, regex)
		} else {
			klog.Errorf("Failed to compile exclude pattern %s: %v", pattern, err)
		}
	}
	
	// Compile timestamp pattern
	if config.Patterns.TimestampFormat != "" {
		// Convert Go time format to regex pattern
		timestampPattern := convertTimeFormatToRegex(config.Patterns.TimestampFormat)
		if regex, err := regexp.Compile(timestampPattern); err == nil {
			parser.timestampRegexp = regex
		} else {
			klog.Errorf("Failed to compile timestamp pattern %s: %v", timestampPattern, err)
		}
	}
	
	// Compile multiline pattern
	if config.Patterns.Multiline && config.Patterns.MultilinePattern != "" {
		if regex, err := regexp.Compile(config.Patterns.MultilinePattern); err == nil {
			parser.multilineRegexp = regex
		} else {
			klog.Errorf("Failed to compile multiline pattern %s: %v", config.Patterns.MultilinePattern, err)
		}
	}
	
	return parser
}

// ParseLine parses a single log line into a LogEntry
func (p *LogParser) ParseLine(line string, sourceFile string, lineNumber int) (*LogEntry, error) {
	if p.shouldExclude(line) {
		return nil, fmt.Errorf("line excluded by patterns")
	}
	
	entry := &LogEntry{
		SourceFile: sourceFile,
		LineNumber: lineNumber,
		RawLine:    line,
		Message:    line,
		CreatedAt:  metav1.Now(),
	}
	
	// Extract timestamp
	if timestamp := p.extractTimestamp(line); !timestamp.IsZero() {
		entry.Timestamp = timestamp
	} else {
		entry.Timestamp = time.Now()
	}
	
	// Extract component and namespace from file path
	if component, namespace := p.extractComponentInfo(sourceFile); component != "" {
		entry.Component = component
		entry.Namespace = namespace
	}
	
	// Determine log level and check for error/warning patterns
	entry.Level = string(p.determineLogLevel(line))
	entry.IsError, entry.ErrorPattern = p.checkErrorPatterns(line)
	entry.IsWarning, _ = p.checkWarningPatterns(line)
	
	// Extract structured message (remove timestamp and level prefixes)
	entry.Message = p.extractMessage(line)
	
	// Extract pod and container information from Kubernetes logs
	if podName, containerName := p.extractK8sInfo(sourceFile, line); podName != "" {
		entry.PodName = podName
		entry.ContainerName = containerName
	}
	
	return entry, nil
}

// ParseMultilineBlock parses multiple related log lines
func (p *LogParser) ParseMultilineBlock(lines []string, sourceFile string, startLineNumber int) (*LogEntry, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty lines block")
	}
	
	// Use the first line as the base entry
	entry, err := p.ParseLine(lines[0], sourceFile, startLineNumber)
	if err != nil {
		return nil, err
	}
	
	// Combine all lines into the message
	entry.Message = strings.Join(lines, "\n")
	entry.RawLine = strings.Join(lines, "\n")
	
	// Re-analyze the combined message for patterns
	entry.IsError, entry.ErrorPattern = p.checkErrorPatterns(entry.Message)
	entry.IsWarning, _ = p.checkWarningPatterns(entry.Message)
	
	return entry, nil
}

// shouldExclude checks if a log line should be excluded
func (p *LogParser) shouldExclude(line string) bool {
	for _, regex := range p.excludeRegexps {
		if regex.MatchString(line) {
			return true
		}
	}
	return false
}

// extractTimestamp attempts to extract timestamp from log line
func (p *LogParser) extractTimestamp(line string) time.Time {
	if p.timestampRegexp != nil {
		if match := p.timestampRegexp.FindString(line); match != "" {
			// Try to parse the extracted timestamp
			if timestamp, err := time.Parse(p.config.Patterns.TimestampFormat, match); err == nil {
				return timestamp
			}
		}
	}
	
	// Fallback: try common timestamp formats
	commonFormats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05",
		"Jan 02 15:04:05",
	}
	
	for _, format := range commonFormats {
		if timestamp := tryParseTimestamp(line, format); !timestamp.IsZero() {
			return timestamp
		}
	}
	
	return time.Time{}
}

// determineLogLevel determines the log level from the line content
func (p *LogParser) determineLogLevel(line string) LogLevel {
	upperLine := strings.ToUpper(line)
	
	if strings.Contains(upperLine, "ERROR") || strings.Contains(upperLine, "FATAL") || strings.Contains(upperLine, "PANIC") {
		return LogLevelError
	}
	if strings.Contains(upperLine, "WARN") || strings.Contains(upperLine, "WARNING") {
		return LogLevelWarning
	}
	if strings.Contains(upperLine, "INFO") {
		return LogLevelInfo
	}
	if strings.Contains(upperLine, "DEBUG") || strings.Contains(upperLine, "TRACE") {
		return LogLevelDebug
	}
	
	return LogLevelUnknown
}

// checkErrorPatterns checks if the line matches any error patterns
func (p *LogParser) checkErrorPatterns(line string) (bool, string) {
	for i, regex := range p.errorRegexps {
		if regex.MatchString(line) {
			if i < len(p.config.Patterns.ErrorPatterns) {
				return true, p.config.Patterns.ErrorPatterns[i]
			}
			return true, ""
		}
	}
	return false, ""
}

// checkWarningPatterns checks if the line matches any warning patterns
func (p *LogParser) checkWarningPatterns(line string) (bool, string) {
	for i, regex := range p.warnRegexps {
		if regex.MatchString(line) {
			if i < len(p.config.Patterns.WarningPatterns) {
				return true, p.config.Patterns.WarningPatterns[i]
			}
			return true, ""
		}
	}
	return false, ""
}

// extractComponentInfo extracts component and namespace from file path
func (p *LogParser) extractComponentInfo(filePath string) (component, namespace string) {
	// For Kubernetes pod logs: /var/log/pods/namespace_podname_uid/container/0.log
	if strings.Contains(filePath, "/pods/") {
		parts := strings.Split(filePath, "/")
		for i, part := range parts {
			if part == "pods" && i+1 < len(parts) {
				// Extract namespace and pod name from namespace_podname_uid format
				podInfo := parts[i+1]
				infoParts := strings.Split(podInfo, "_")
				if len(infoParts) >= 2 {
					namespace = infoParts[0]
					component = infoParts[1]
				}
				break
			}
		}
	}
	
	return component, namespace
}

// extractK8sInfo extracts pod and container information from Kubernetes logs
func (p *LogParser) extractK8sInfo(filePath, line string) (podName, containerName string) {
	// Extract from file path: /var/log/pods/namespace_podname_uid/container/0.log
	if strings.Contains(filePath, "/pods/") {
		parts := strings.Split(filePath, "/")
		for i, part := range parts {
			if part == "pods" && i+2 < len(parts) {
				podInfo := parts[i+1]
				containerName = parts[i+2]
				
				// Extract pod name from namespace_podname_uid
				infoParts := strings.Split(podInfo, "_")
				if len(infoParts) >= 2 {
					podName = infoParts[1]
				}
				break
			}
		}
	}
	
	return podName, containerName
}

// extractMessage extracts the actual log message, removing prefixes
func (p *LogParser) extractMessage(line string) string {
	// Remove common log prefixes (timestamp, level, etc.)
	message := line
	
	// Remove timestamp prefix if present
	if p.timestampRegexp != nil {
		message = p.timestampRegexp.ReplaceAllString(message, "")
	}
	
	// Remove common level prefixes
	levelPrefixes := []string{
		"ERROR:", "WARN:", "INFO:", "DEBUG:",
		"[ERROR]", "[WARN]", "[INFO]", "[DEBUG]",
		"E:", "W:", "I:", "D:",
	}
	
	for _, prefix := range levelPrefixes {
		if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(message)), prefix) {
			message = strings.TrimSpace(message[len(prefix):])
			break
		}
	}
	
	return strings.TrimSpace(message)
}

// isMultilineStart checks if a line starts a multiline block
func (p *LogParser) IsMultilineStart(line string) bool {
	if !p.config.Patterns.Multiline || p.multilineRegexp == nil {
		return false
	}
	
	return p.multilineRegexp.MatchString(line)
}

// Helper functions

// convertTimeFormatToRegex converts Go time format to regex pattern
func convertTimeFormatToRegex(format string) string {
	// This is a simplified conversion, could be enhanced
	pattern := format
	replacements := map[string]string{
		"2006": `\d{4}`,
		"01":   `\d{2}`,
		"02":   `\d{2}`,
		"15":   `\d{2}`,
		"04":   `\d{2}`,
		"05":   `\d{2}`,
		"000":  `\d{3}`,
	}
	
	for old, new := range replacements {
		pattern = strings.ReplaceAll(pattern, old, new)
	}
	
	return pattern
}

// tryParseTimestamp attempts to parse timestamp from line using given format
func tryParseTimestamp(line, format string) time.Time {
	// Simple extraction - look for timestamp-like patterns at the beginning of line
	words := strings.Fields(line)
	if len(words) == 0 {
		return time.Time{}
	}
	
	// Try first few words as potential timestamps
	for i := 0; i < len(words) && i < 3; i++ {
		candidate := strings.Join(words[:i+1], " ")
		if timestamp, err := time.Parse(format, candidate); err == nil {
			return timestamp
		}
	}
	
	return time.Time{}
}