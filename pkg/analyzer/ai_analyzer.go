package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
	"k8s.io/klog/v2"

	"milvus-coredump-agent/pkg/collector"
	"milvus-coredump-agent/pkg/config"
)

type AIAnalyzer struct {
	config        *config.AIAnalysisConfig
	client        *openai.Client
	
	// Cost control
	mu            sync.RWMutex
	monthlyUsage  float64
	hourlyCount   int
	lastHourReset time.Time
}

func NewAIAnalyzer(config *config.AIAnalysisConfig) (*AIAnalyzer, error) {
	if !config.Enabled {
		return &AIAnalyzer{config: config}, nil
	}

	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key not provided")
	}

	clientConfig := openai.DefaultConfig(apiKey)
	if config.BaseURL != "" {
		clientConfig.BaseURL = config.BaseURL
	}

	client := openai.NewClientWithConfig(clientConfig)

	return &AIAnalyzer{
		config:        config,
		client:        client,
		lastHourReset: time.Now(),
	}, nil
}

func (ai *AIAnalyzer) AnalyzeCoredump(ctx context.Context, coredump *collector.CoredumpFile, gdbResults *collector.AnalysisResults) (*collector.AIAnalysisResult, error) {
	if !ai.config.Enabled || ai.client == nil {
		return &collector.AIAnalysisResult{
			Enabled: false,
		}, nil
	}

	// Check cost control
	if !ai.checkCostLimits() {
		klog.V(2).Infof("AI analysis skipped due to cost control limits")
		return &collector.AIAnalysisResult{
			Enabled:      true,
			Provider:     ai.config.Provider,
			Model:        ai.config.Model,
			AnalysisTime: time.Now(),
			ErrorMessage: "Analysis skipped due to cost control limits",
		}, nil
	}

	startTime := time.Now()
	
	prompt := ai.buildAnalysisPrompt(coredump, gdbResults)
	
	chatCtx, cancel := context.WithTimeout(ctx, ai.config.Timeout)
	defer cancel()

	resp, err := ai.client.CreateChatCompletion(chatCtx, openai.ChatCompletionRequest{
		Model:       ai.config.Model,
		Temperature: ai.config.Temperature,
		MaxTokens:   ai.config.MaxTokens,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: ai.getSystemPrompt(),
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
	})

	if err != nil {
		klog.Errorf("OpenAI API error: %v", err)
		return &collector.AIAnalysisResult{
			Enabled:      true,
			Provider:     ai.config.Provider,
			Model:        ai.config.Model,
			AnalysisTime: time.Now(),
			ErrorMessage: fmt.Sprintf("API error: %v", err),
		}, nil
	}

	if len(resp.Choices) == 0 {
		return &collector.AIAnalysisResult{
			Enabled:      true,
			Provider:     ai.config.Provider,
			Model:        ai.config.Model,
			AnalysisTime: time.Now(),
			ErrorMessage: "No response from AI model",
		}, nil
	}

	analysis, err := ai.parseAIResponse(resp.Choices[0].Message.Content)
	if err != nil {
		klog.Errorf("Failed to parse AI response: %v", err)
		analysis = &collector.AIAnalysisResult{
			Summary: resp.Choices[0].Message.Content, // Fallback to raw response
		}
	}

	// Fill in metadata
	analysis.Enabled = true
	analysis.Provider = ai.config.Provider
	analysis.Model = ai.config.Model
	analysis.AnalysisTime = startTime
	analysis.TokensUsed = resp.Usage.TotalTokens
	analysis.CostUSD = ai.calculateCost(resp.Usage.TotalTokens)

	// Update cost tracking
	ai.updateUsage(analysis.CostUSD)

	klog.Infof("AI analysis completed for %s: cost=$%.4f, tokens=%d, duration=%v", 
		coredump.Path, analysis.CostUSD, analysis.TokensUsed, time.Since(startTime))

	return analysis, nil
}

func (ai *AIAnalyzer) getSystemPrompt() string {
	return `You are an expert system debugger specializing in analyzing coredump files and stack traces from C/C++ applications, particularly vector databases like Milvus.

Your task is to analyze the provided coredump information and provide structured insights that will help developers debug the issue.

Please respond in JSON format with the following structure:
{
  "summary": "Brief summary of the crash",
  "rootCause": "Most likely root cause of the crash", 
  "impact": "Impact assessment of this crash",
  "recommendations": ["List", "of", "actionable", "recommendations"],
  "confidence": 0.85,
  "relatedIssues": ["Known similar issues or patterns"],
  "codeSuggestions": [
    {
      "file": "suspected_file.cpp",
      "function": "function_name", 
      "lineNumber": 123,
      "issue": "Description of the issue",
      "suggestion": "Specific code fix suggestion",
      "priority": "high"
    }
  ]
}

Focus on:
1. Memory access violations (SIGSEGV, SIGBUS)
2. Assertion failures and abort signals (SIGABRT)
3. Threading issues and race conditions
4. Memory leaks and corruption
5. Vector database specific issues (indexing, search, data corruption)
6. Performance bottlenecks leading to crashes

Be precise and actionable in your recommendations.`
}

func (ai *AIAnalyzer) buildAnalysisPrompt(coredump *collector.CoredumpFile, gdbResults *collector.AnalysisResults) string {
	var prompt strings.Builder
	
	prompt.WriteString("COREDUMP ANALYSIS REQUEST\n")
	prompt.WriteString("========================\n\n")
	
	// Basic info
	prompt.WriteString(fmt.Sprintf("Application: %s\n", coredump.Executable))
	prompt.WriteString(fmt.Sprintf("Signal: %d (%s)\n", coredump.Signal, ai.getSignalName(coredump.Signal)))
	prompt.WriteString(fmt.Sprintf("PID: %d\n", coredump.PID))
	if coredump.PodName != "" {
		prompt.WriteString(fmt.Sprintf("Kubernetes Pod: %s/%s\n", coredump.PodNamespace, coredump.PodName))
		prompt.WriteString(fmt.Sprintf("Milvus Instance: %s\n", coredump.InstanceName))
	}
	prompt.WriteString("\n")

	// GDB Analysis Results
	if gdbResults != nil {
		if gdbResults.CrashReason != "" {
			prompt.WriteString(fmt.Sprintf("Crash Reason: %s\n", gdbResults.CrashReason))
		}
		if gdbResults.CrashAddress != "" {
			prompt.WriteString(fmt.Sprintf("Crash Address: %s\n", gdbResults.CrashAddress))
		}
		prompt.WriteString(fmt.Sprintf("Thread Count: %d\n", gdbResults.ThreadCount))
		prompt.WriteString("\n")

		// Stack trace (most important for AI analysis)
		if gdbResults.StackTrace != "" {
			prompt.WriteString("STACK TRACE:\n")
			prompt.WriteString("```\n")
			// Limit stack trace to avoid token limits
			stackTrace := gdbResults.StackTrace
			if len(stackTrace) > 3000 {
				stackTrace = stackTrace[:3000] + "\n... [truncated]"
			}
			prompt.WriteString(stackTrace)
			prompt.WriteString("\n```\n\n")
		}

		// Register info (key registers only)
		if len(gdbResults.RegisterInfo) > 0 {
			prompt.WriteString("KEY REGISTERS:\n")
			keyRegs := []string{"rip", "rsp", "rbp", "rax", "rcx", "rdx"}
			for _, reg := range keyRegs {
				if val, exists := gdbResults.RegisterInfo[reg]; exists {
					prompt.WriteString(fmt.Sprintf("%s = %s\n", reg, val))
				}
			}
			prompt.WriteString("\n")
		}

		// Shared libraries
		if len(gdbResults.SharedLibraries) > 0 {
			prompt.WriteString("LOADED LIBRARIES:\n")
			for i, lib := range gdbResults.SharedLibraries {
				if i >= 10 { // Limit to first 10 libraries
					prompt.WriteString("... [and more]\n")
					break
				}
				prompt.WriteString(fmt.Sprintf("- %s\n", lib))
			}
			prompt.WriteString("\n")
		}
	}

	prompt.WriteString("Please analyze this coredump and provide structured debugging insights in JSON format.")
	
	return prompt.String()
}

func (ai *AIAnalyzer) parseAIResponse(response string) (*collector.AIAnalysisResult, error) {
	// Try to extract JSON from the response
	response = strings.TrimSpace(response)
	
	// Find JSON block if response contains additional text
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	
	if start == -1 || end == -1 || start >= end {
		return nil, fmt.Errorf("no valid JSON found in response")
	}
	
	jsonStr := response[start : end+1]
	
	var result struct {
		Summary         string                     `json:"summary"`
		RootCause       string                     `json:"rootCause"`
		Impact          string                     `json:"impact"`
		Recommendations []string                   `json:"recommendations"`
		Confidence      float64                    `json:"confidence"`
		RelatedIssues   []string                   `json:"relatedIssues"`
		CodeSuggestions []collector.CodeSuggestion `json:"codeSuggestions"`
	}
	
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	
	return &collector.AIAnalysisResult{
		Summary:         result.Summary,
		RootCause:       result.RootCause,
		Impact:          result.Impact,
		Recommendations: result.Recommendations,
		Confidence:      result.Confidence,
		RelatedIssues:   result.RelatedIssues,
		CodeSuggestions: result.CodeSuggestions,
	}, nil
}

func (ai *AIAnalyzer) getSignalName(signal int) string {
	signals := map[int]string{
		1:  "SIGHUP",
		2:  "SIGINT", 
		3:  "SIGQUIT",
		4:  "SIGILL",
		6:  "SIGABRT",
		7:  "SIGBUS",
		8:  "SIGFPE",
		9:  "SIGKILL",
		11: "SIGSEGV",
		13: "SIGPIPE",
		14: "SIGALRM",
		15: "SIGTERM",
	}
	
	if name, exists := signals[signal]; exists {
		return name
	}
	return fmt.Sprintf("Signal %d", signal)
}

func (ai *AIAnalyzer) calculateCost(tokens int) float64 {
	// OpenAI GPT-4 pricing (as of 2024)
	// Input: $0.03/1K tokens, Output: $0.06/1K tokens
	// Simplified calculation assuming 50/50 split
	costPer1KTokens := 0.045 // Average of input and output costs
	return float64(tokens) / 1000.0 * costPer1KTokens
}

func (ai *AIAnalyzer) checkCostLimits() bool {
	if !ai.config.EnableCostControl {
		return true
	}

	ai.mu.Lock()
	defer ai.mu.Unlock()

	// Reset hourly counter if needed
	if time.Since(ai.lastHourReset) > time.Hour {
		ai.hourlyCount = 0
		ai.lastHourReset = time.Now()
	}

	// Check hourly limit
	if ai.hourlyCount >= ai.config.MaxAnalysisPerHour {
		return false
	}

	// Check monthly cost limit
	if ai.monthlyUsage >= ai.config.MaxCostPerMonth {
		return false
	}

	return true
}

func (ai *AIAnalyzer) updateUsage(cost float64) {
	if !ai.config.EnableCostControl {
		return
	}

	ai.mu.Lock()
	defer ai.mu.Unlock()

	ai.monthlyUsage += cost
	ai.hourlyCount++
}

func (ai *AIAnalyzer) GetUsageStats() (monthlyUsage float64, hourlyCount int) {
	ai.mu.RLock()
	defer ai.mu.RUnlock()
	
	return ai.monthlyUsage, ai.hourlyCount
}