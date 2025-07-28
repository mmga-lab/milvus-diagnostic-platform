package analyzer

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"milvus-coredump-agent/pkg/collector"
	"milvus-coredump-agent/pkg/config"
)

type Analyzer struct {
	config     *config.AnalyzerConfig
	eventChan  chan AnalysisEvent
	aiAnalyzer *AIAnalyzer
}

type AnalysisEvent struct {
	Type         EventType                `json:"type"`
	CoredumpFile *collector.CoredumpFile  `json:"coredumpFile"`
	Error        string                   `json:"error,omitempty"`
	Timestamp    time.Time                `json:"timestamp"`
}

type EventType string

const (
	EventTypeAnalysisComplete EventType = "analysis_complete"
	EventTypeAnalysisSkipped  EventType = "analysis_skipped"
	EventTypeAnalysisError    EventType = "analysis_error"
)

func New(config *config.AnalyzerConfig) *Analyzer {
	aiAnalyzer, err := NewAIAnalyzer(&config.AIAnalysis)
	if err != nil {
		klog.Errorf("Failed to initialize AI analyzer: %v", err)
		// Continue without AI analysis
		aiAnalyzer = nil
	}

	return &Analyzer{
		config:     config,
		eventChan:  make(chan AnalysisEvent, 100),
		aiAnalyzer: aiAnalyzer,
	}
}

func (a *Analyzer) Start(ctx context.Context, collectorChan <-chan collector.CollectionEvent) error {
	klog.Info("Starting coredump analyzer")

	go a.processCollectionEvents(ctx, collectorChan)

	<-ctx.Done()
	return nil
}

func (a *Analyzer) GetEventChannel() <-chan AnalysisEvent {
	return a.eventChan
}

func (a *Analyzer) processCollectionEvents(ctx context.Context, collectorChan <-chan collector.CollectionEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-collectorChan:
			if event.Type == collector.EventTypeFileDiscovered && event.CoredumpFile != nil {
				go a.analyzeCoredumpFile(event.CoredumpFile)
			}
		}
	}
}

func (a *Analyzer) analyzeCoredumpFile(coredump *collector.CoredumpFile) {
	klog.Infof("Analyzing coredump file: %s", coredump.Path)

	if a.shouldSkipAnalysis(coredump) {
		coredump.Status = collector.StatusSkipped
		coredump.UpdatedAt = metav1.Now()
		
		event := AnalysisEvent{
			Type:         EventTypeAnalysisSkipped,
			CoredumpFile: coredump,
			Timestamp:    time.Now(),
		}
		
		a.sendEvent(event)
		return
	}

	coredump.Status = collector.StatusProcessing
	coredump.UpdatedAt = metav1.Now()

	var analysisResults *collector.AnalysisResults
	var err error

	if a.config.EnableGdbAnalysis {
		analysisResults, err = a.analyzeWithGdb(coredump)
	} else {
		analysisResults, err = a.basicAnalysis(coredump)
	}

	if err != nil {
		klog.Errorf("Failed to analyze coredump %s: %v", coredump.Path, err)
		coredump.Status = collector.StatusError
		coredump.ErrorMessage = err.Error()
		coredump.UpdatedAt = metav1.Now()
		
		event := AnalysisEvent{
			Type:         EventTypeAnalysisError,
			CoredumpFile: coredump,
			Error:        err.Error(),
			Timestamp:    time.Now(),
		}
		
		a.sendEvent(event)
		return
	}

	// Perform AI analysis if available and enabled
	if a.aiAnalyzer != nil {
		klog.V(2).Infof("Starting AI analysis for %s", coredump.Path)
		
		aiCtx, aiCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer aiCancel()
		
		aiResult, aiErr := a.aiAnalyzer.AnalyzeCoredump(aiCtx, coredump, analysisResults)
		if aiErr != nil {
			klog.Errorf("AI analysis failed for %s: %v", coredump.Path, aiErr)
			// Don't fail the entire analysis, just log the error
			if analysisResults != nil {
				analysisResults.AIAnalysis = &collector.AIAnalysisResult{
					Enabled:      true,
					Provider:     a.config.AIAnalysis.Provider,
					Model:        a.config.AIAnalysis.Model,
					AnalysisTime: time.Now(),
					ErrorMessage: fmt.Sprintf("AI analysis failed: %v", aiErr),
				}
			}
		} else if aiResult != nil {
			if analysisResults != nil {
				analysisResults.AIAnalysis = aiResult
			}
			klog.Infof("AI analysis completed for %s: confidence=%.2f, cost=$%.4f", 
				coredump.Path, aiResult.Confidence, aiResult.CostUSD)
		}
	}

	coredump.AnalysisResults = analysisResults
	coredump.ValueScore = a.calculateValueScore(coredump, analysisResults)
	coredump.IsAnalyzed = true
	coredump.AnalysisTime = time.Now()
	coredump.Status = collector.StatusAnalyzed
	coredump.UpdatedAt = metav1.Now()

	klog.Infof("Analysis complete for %s, value score: %.2f", coredump.Path, coredump.ValueScore)

	event := AnalysisEvent{
		Type:         EventTypeAnalysisComplete,
		CoredumpFile: coredump,
		Timestamp:    time.Now(),
	}
	
	a.sendEvent(event)
}

func (a *Analyzer) shouldSkipAnalysis(coredump *collector.CoredumpFile) bool {
	if coredump.ContainerName != "" {
		for _, pattern := range a.config.IgnorePatterns {
			if strings.Contains(coredump.ContainerName, pattern) {
				klog.V(2).Infof("Skipping analysis for %s due to ignore pattern: %s", 
					coredump.Path, pattern)
				return true
			}
		}
	}

	maxSize := int64(2 * 1024 * 1024 * 1024) // 2GB
	if coredump.Size > maxSize {
		klog.V(2).Infof("Skipping analysis for %s due to large size: %d bytes", 
			coredump.Path, coredump.Size)
		return true
	}

	if time.Since(coredump.ModTime) > 24*time.Hour {
		klog.V(2).Infof("Skipping analysis for %s due to old age", coredump.Path)
		return true
	}

	return false
}

func (a *Analyzer) analyzeWithGdb(coredump *collector.CoredumpFile) (*collector.AnalysisResults, error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.config.GdbTimeout)
	defer cancel()

	gdbScript := a.generateGdbScript()
	
	cmd := exec.CommandContext(ctx, "gdb", "-batch", "-x", "-", coredump.Path)
	cmd.Stdin = strings.NewReader(gdbScript)
	
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gdb analysis failed: %w", err)
	}

	return a.parseGdbOutput(string(output))
}

func (a *Analyzer) generateGdbScript() string {
	return `
set pagination off
set logging file /dev/stdout
set logging on

echo =====BACKTRACE=====\n
bt full
echo =====REGISTERS=====\n
info registers
echo =====THREADS=====\n
info threads
bt
echo =====MEMORY=====\n
info proc mappings
echo =====SHARED_LIBS=====\n
info sharedlibrary
echo =====END=====\n
quit
`
}

func (a *Analyzer) parseGdbOutput(output string) (*collector.AnalysisResults, error) {
	results := &collector.AnalysisResults{
		LibraryVersions: make(map[string]string),
		RegisterInfo:    make(map[string]string),
		SharedLibraries: []string{},
	}

	sections := a.splitGdbOutput(output)
	
	if backtrace, exists := sections["BACKTRACE"]; exists {
		results.StackTrace = backtrace
		results.CrashReason = a.extractCrashReason(backtrace)
		results.CrashAddress = a.extractCrashAddress(backtrace)
	}

	if registers, exists := sections["REGISTERS"]; exists {
		results.RegisterInfo = a.parseRegisterInfo(registers)
	}

	if threads, exists := sections["THREADS"]; exists {
		results.ThreadCount = a.countThreads(threads)
	}

	if memory, exists := sections["MEMORY"]; exists {
		results.MemoryInfo = a.parseMemoryInfo(memory)
	}

	if sharedLibs, exists := sections["SHARED_LIBS"]; exists {
		results.SharedLibraries = a.parseSharedLibraries(sharedLibs)
	}

	return results, nil
}

func (a *Analyzer) basicAnalysis(coredump *collector.CoredumpFile) (*collector.AnalysisResults, error) {
	results := &collector.AnalysisResults{
		LibraryVersions: make(map[string]string),
		RegisterInfo:    make(map[string]string),
		SharedLibraries: []string{},
	}

	results.CrashReason = a.inferCrashReasonFromSignal(coredump.Signal)
	
	fileCmd := exec.Command("file", coredump.Path)
	if output, err := fileCmd.Output(); err == nil {
		if strings.Contains(string(output), "from") {
			results.CrashAddress = a.extractAddressFromFile(string(output))
		}
	}

	results.ThreadCount = 1

	return results, nil
}

func (a *Analyzer) calculateValueScore(coredump *collector.CoredumpFile, results *collector.AnalysisResults) float64 {
	score := 5.0 // base score

	// Traditional GDB analysis scoring
	if results.CrashReason != "" {
		score += 2.0
		
		for _, keyword := range a.config.PanicKeywords {
			if strings.Contains(strings.ToLower(results.CrashReason), strings.ToLower(keyword)) {
				score += 1.0
				break
			}
		}
	}

	if results.StackTrace != "" && len(results.StackTrace) > 100 {
		score += 1.5
	}

	if results.ThreadCount > 1 {
		score += 0.5
	}

	if coredump.PodName != "" && coredump.InstanceName != "" {
		score += 1.0
	}

	if coredump.Signal == 11 || coredump.Signal == 6 || coredump.Signal == 8 {
		score += 1.0
	}

	if coredump.Size > 100*1024*1024 {
		score += 0.5
	}

	if time.Since(coredump.ModTime) < time.Hour {
		score += 0.5
	}

	// AI analysis bonus scoring
	if results.AIAnalysis != nil && results.AIAnalysis.Enabled && results.AIAnalysis.ErrorMessage == "" {
		// High confidence AI analysis adds significant value
		if results.AIAnalysis.Confidence > 0.8 {
			score += 1.5
		} else if results.AIAnalysis.Confidence > 0.6 {
			score += 1.0
		} else if results.AIAnalysis.Confidence > 0.4 {
			score += 0.5
		}

		// Actionable recommendations boost value
		if len(results.AIAnalysis.Recommendations) > 0 {
			score += 0.5
		}

		// Code suggestions indicate concrete actionable insights
		if len(results.AIAnalysis.CodeSuggestions) > 0 {
			score += 0.5
			// High priority suggestions add more value
			for _, suggestion := range results.AIAnalysis.CodeSuggestions {
				if suggestion.Priority == "high" {
					score += 0.3
					break
				}
			}
		}

		// Related issues suggest this is a known problem pattern
		if len(results.AIAnalysis.RelatedIssues) > 0 {
			score += 0.3
		}
	}

	if score > 10.0 {
		score = 10.0
	}

	return score
}

func (a *Analyzer) splitGdbOutput(output string) map[string]string {
	sections := make(map[string]string)
	
	lines := strings.Split(output, "\n")
	var currentSection string
	var currentContent []string
	
	for _, line := range lines {
		if strings.HasPrefix(line, "=====") && strings.HasSuffix(line, "=====") {
			if currentSection != "" {
				sections[currentSection] = strings.Join(currentContent, "\n")
			}
			currentSection = strings.Trim(line, "=")
			currentContent = []string{}
		} else if currentSection != "" {
			currentContent = append(currentContent, line)
		}
	}
	
	if currentSection != "" {
		sections[currentSection] = strings.Join(currentContent, "\n")
	}
	
	return sections
}

func (a *Analyzer) extractCrashReason(backtrace string) string {
	lines := strings.Split(backtrace, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "SIGSEGV") {
			return "Segmentation fault (SIGSEGV)"
		}
		if strings.Contains(line, "SIGABRT") {
			return "Abort signal (SIGABRT)"
		}
		if strings.Contains(line, "SIGFPE") {
			return "Floating point exception (SIGFPE)"
		}
		if strings.Contains(line, "assert") {
			return "Assertion failure"
		}
	}
	return "Unknown crash reason"
}

func (a *Analyzer) extractCrashAddress(backtrace string) string {
	re := regexp.MustCompile(`0x[0-9a-fA-F]+`)
	matches := re.FindAllString(backtrace, -1)
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

func (a *Analyzer) parseRegisterInfo(registers string) map[string]string {
	registerMap := make(map[string]string)
	
	lines := strings.Split(registers, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				registerMap[key] = value
			}
		}
	}
	
	return registerMap
}

func (a *Analyzer) countThreads(threads string) int {
	count := 0
	lines := strings.Split(threads, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Thread") {
			count++
		}
	}
	if count == 0 {
		count = 1
	}
	return count
}

func (a *Analyzer) parseMemoryInfo(memory string) collector.MemoryInfo {
	memInfo := collector.MemoryInfo{}
	
	lines := strings.Split(memory, "\n")
	for _, line := range lines {
		if strings.Contains(line, "heap") {
			if size := a.extractSizeFromLine(line); size > 0 {
				memInfo.HeapSize = size
			}
		}
		if strings.Contains(line, "stack") {
			if size := a.extractSizeFromLine(line); size > 0 {
				memInfo.StackSize = size
			}
		}
	}
	
	return memInfo
}

func (a *Analyzer) parseSharedLibraries(sharedLibs string) []string {
	var libraries []string
	
	lines := strings.Split(sharedLibs, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, ".so") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				libraries = append(libraries, parts[len(parts)-1])
			}
		}
	}
	
	return libraries
}

func (a *Analyzer) inferCrashReasonFromSignal(signal int) string {
	switch signal {
	case 11:
		return "Segmentation fault (SIGSEGV)"
	case 6:
		return "Abort signal (SIGABRT)"
	case 8:
		return "Floating point exception (SIGFPE)"
	case 4:
		return "Illegal instruction (SIGILL)"
	case 7:
		return "Bus error (SIGBUS)"
	default:
		return fmt.Sprintf("Signal %d", signal)
	}
}

func (a *Analyzer) extractAddressFromFile(output string) string {
	re := regexp.MustCompile(`from '([^']+)'`)
	matches := re.FindStringSubmatch(output)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func (a *Analyzer) extractSizeFromLine(line string) int64 {
	re := regexp.MustCompile(`(\d+)`)
	matches := re.FindAllString(line, -1)
	if len(matches) > 0 {
		if size, err := strconv.ParseInt(matches[0], 10, 64); err == nil {
			return size
		}
	}
	return 0
}

func (a *Analyzer) sendEvent(event AnalysisEvent) {
	select {
	case a.eventChan <- event:
	default:
		klog.Warning("Analysis event channel is full, dropping event")
	}
}