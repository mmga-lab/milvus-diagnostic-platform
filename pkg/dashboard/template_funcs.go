package dashboard

import (
	"fmt"
	"html/template"
	"net/url"
	"time"
)

// GetTemplateFuncs returns template functions for HTML rendering
func GetTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatTime":     formatTime,
		"formatFileSize": formatFileSize,
		"urlEncode":      urlEncode,
		"add":            add,
		"sub":            sub,
		"mul":            mul,
		"min":            min,
		"max":            max,
	}
}

// formatTime formats time for display
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	
	now := time.Now()
	diff := now.Sub(t)
	
	if diff < time.Minute {
		return fmt.Sprintf("%d秒前", int(diff.Seconds()))
	} else if diff < time.Hour {
		return fmt.Sprintf("%d分钟前", int(diff.Minutes()))
	} else if diff < 24*time.Hour {
		return fmt.Sprintf("%d小时前", int(diff.Hours()))
	} else if diff < 7*24*time.Hour {
		return fmt.Sprintf("%d天前", int(diff.Hours()/24))
	} else {
		return t.Format("2006-01-02 15:04:05")
	}
}

// formatFileSize formats file size for display
func formatFileSize(size int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	
	if size < KB {
		return fmt.Sprintf("%dB", size)
	} else if size < MB {
		return fmt.Sprintf("%.1fKB", float64(size)/KB)
	} else if size < GB {
		return fmt.Sprintf("%.1fMB", float64(size)/MB)
	} else {
		return fmt.Sprintf("%.1fGB", float64(size)/GB)
	}
}

// urlEncode URL encodes a string
func urlEncode(s string) string {
	return url.QueryEscape(s)
}

// Mathematical helper functions for templates
func add(a, b int) int {
	return a + b
}

func sub(a, b int) int {
	return a - b
}

func mul(a, b int) int {
	return a * b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}