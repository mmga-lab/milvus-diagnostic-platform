package dashboard

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

type Handlers struct {
	aggregator *DataAggregator
	viewer     *CoredumpViewer
	templates  *template.Template
}

func NewHandlers(aggregator *DataAggregator, viewer *CoredumpViewer) *Handlers {
	// Load HTML templates with custom functions
	templates := template.New("")
	templates.Funcs(GetTemplateFuncs())
	templates = template.Must(templates.ParseGlob("web/templates/*.html"))
	
	return &Handlers{
		aggregator: aggregator,
		viewer:     viewer,
		templates:  templates,
	}
}

// 中间件：CORS和日志
func (h *Handlers) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		
		klog.V(4).Infof("API %s %s - %v", r.Method, r.URL.Path, duration)
	}
}

// 响应辅助函数
func (h *Handlers) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		klog.Errorf("Failed to encode JSON response: %v", err)
	}
}

func (h *Handlers) writeError(w http.ResponseWriter, status int, message string) {
	h.writeJSON(w, status, APIError{
		Error:   http.StatusText(status),
		Code:    status,
		Message: message,
	})
}

func (h *Handlers) writeHTML(w http.ResponseWriter, status int, templateName string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	
	var buf bytes.Buffer
	if err := h.templates.ExecuteTemplate(&buf, templateName, data); err != nil {
		klog.Errorf("Failed to execute template %s: %v", templateName, err)
		http.Error(w, "Template execution failed", http.StatusInternalServerError)
		return
	}
	
	w.Write(buf.Bytes())
}

func (h *Handlers) isHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// 解析分页参数
func (h *Handlers) parsePaginationParams(r *http.Request) PaginationParams {
	params := PaginationParams{
		Page:     1,
		PageSize: 20,
		SortBy:   "name",
		SortDesc: false,
	}
	
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if page, err := strconv.Atoi(pageStr); err == nil && page > 0 {
			params.Page = page
		}
	}
	
	if sizeStr := r.URL.Query().Get("pageSize"); sizeStr != "" {
		if size, err := strconv.Atoi(sizeStr); err == nil && size > 0 && size <= 100 {
			params.PageSize = size
		}
	}
	
	if sortBy := r.URL.Query().Get("sortBy"); sortBy != "" {
		params.SortBy = sortBy
	}
	
	if sortDesc := r.URL.Query().Get("sortDesc"); sortDesc == "true" {
		params.SortDesc = true
	}
	
	return params
}

// API处理器

// GET /dashboard - Dashboard总览页面（HTMX） 
func (h *Handlers) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	summary := h.aggregator.GetSummary()
	metrics := h.aggregator.GetMetrics()
	
	// Get recent activities (last 5 coredumps)
	recentCoredumps := h.aggregator.GetCoredumps(PaginationParams{
		Page: 1, PageSize: 5, SortBy: "time", SortDesc: true,
	})
	
	data := map[string]interface{}{
		"Summary": summary,
		"Metrics": metrics,
		"RecentActivities": recentCoredumps.Data,
	}
	
	h.writeHTML(w, http.StatusOK, "dashboard.html", data)
}

// GET /api/v1/summary - 获取Dashboard总览数据（JSON API）
func (h *Handlers) HandleSummary(w http.ResponseWriter, r *http.Request) {
	summary := h.aggregator.GetSummary()
	h.writeJSON(w, http.StatusOK, summary)
}

// GET /instances - 实例列表页面（HTMX）
func (h *Handlers) HandleInstancesPage(w http.ResponseWriter, r *http.Request) {
	params := h.parsePaginationParams(r)
	result := h.aggregator.GetInstances(params)
	h.writeHTML(w, http.StatusOK, "instances.html", result)
}

// GET /api/v1/instances - 获取Milvus实例列表（JSON API）
func (h *Handlers) HandleInstances(w http.ResponseWriter, r *http.Request) {
	params := h.parsePaginationParams(r)
	result := h.aggregator.GetInstances(params)
	h.writeJSON(w, http.StatusOK, result)
}

// GET /api/v1/instances/{name}/pods - 获取实例的Pod详情
func (h *Handlers) HandleInstancePods(w http.ResponseWriter, r *http.Request) {
	// 从URL路径中提取实例名称
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/instances/"), "/")
	if len(pathParts) < 2 || pathParts[1] != "pods" {
		h.writeError(w, http.StatusBadRequest, "Invalid path format")
		return
	}
	
	instanceName := pathParts[0]
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}
	
	// 查找实例
	instances := h.aggregator.GetInstances(PaginationParams{Page: 1, PageSize: 1000})
	var targetInstance *InstanceOverview
	
	for _, item := range instances.Data.([]InstanceOverview) {
		if item.Instance.Name == instanceName && item.Instance.Namespace == namespace {
			targetInstance = &item
			break
		}
	}
	
	if targetInstance == nil {
		h.writeError(w, http.StatusNotFound, "Instance not found")
		return
	}
	
	h.writeJSON(w, http.StatusOK, targetInstance.Instance.Pods)
}

// GET /coredumps - Coredump列表页面（HTMX）
func (h *Handlers) HandleCoredumpsPage(w http.ResponseWriter, r *http.Request) {
	params := h.parsePaginationParams(r)
	
	// 添加过滤参数
	if instance := r.URL.Query().Get("instance"); instance != "" {
		// TODO: 在DataAggregator中实现按实例过滤
	}
	
	if minScore := r.URL.Query().Get("minScore"); minScore != "" {
		// TODO: 在DataAggregator中实现按分数过滤
	}
	
	result := h.aggregator.GetCoredumps(params)
	h.writeHTML(w, http.StatusOK, "coredumps.html", result)
}

// GET /api/v1/coredumps - 获取Coredump文件列表（JSON API）
func (h *Handlers) HandleCoredumps(w http.ResponseWriter, r *http.Request) {
	params := h.parsePaginationParams(r)
	
	// 添加过滤参数
	if instance := r.URL.Query().Get("instance"); instance != "" {
		// TODO: 在DataAggregator中实现按实例过滤
	}
	
	if minScore := r.URL.Query().Get("minScore"); minScore != "" {
		// TODO: 在DataAggregator中实现按分数过滤
	}
	
	result := h.aggregator.GetCoredumps(params)
	h.writeJSON(w, http.StatusOK, result)
}

// GET /api/v1/coredumps/{id} - 获取单个Coredump详情
func (h *Handlers) HandleCoredumpDetail(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/coredumps/"), "/")
	if len(pathParts) < 1 {
		h.writeError(w, http.StatusBadRequest, "Coredump ID required")
		return
	}
	
	coredumpID := pathParts[0]
	// 在实际实现中，ID应该是URL编码的文件路径
	coredumpPath := coredumpID // 简化处理，实际需要URL解码
	
	detail, err := h.aggregator.GetCoredumpDetail(coredumpPath)
	if err != nil {
		h.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	
	h.writeJSON(w, http.StatusOK, detail)
}

// POST /api/v1/coredumps/{id}/view - 创建Coredump查看器
func (h *Handlers) HandleCreateViewer(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/coredumps/"), "/")
	if len(pathParts) < 2 || pathParts[1] != "view" {
		h.writeError(w, http.StatusBadRequest, "Invalid path format")
		return
	}
	
	coredumpID := pathParts[0]
	
	var req ViewerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	
	if req.Duration == 0 {
		req.Duration = 30 // 默认30分钟
	}
	
	req.CoredumpID = coredumpID
	
	response, err := h.viewer.CreateViewer(r.Context(), req)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	
	h.writeJSON(w, http.StatusCreated, response)
}

// GET /api/v1/viewers/{id} - 获取查看器状态
func (h *Handlers) HandleViewerStatus(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/viewers/"), "/")
	if len(pathParts) < 1 {
		h.writeError(w, http.StatusBadRequest, "Viewer ID required")
		return
	}
	
	viewerID := pathParts[0]
	
	status, err := h.viewer.GetViewerStatus(viewerID)
	if err != nil {
		h.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	
	h.writeJSON(w, http.StatusOK, status)
}

// DELETE /api/v1/viewers/{id} - 停止查看器
func (h *Handlers) HandleStopViewer(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/viewers/"), "/")
	if len(pathParts) < 1 {
		h.writeError(w, http.StatusBadRequest, "Viewer ID required")
		return
	}
	
	viewerID := pathParts[0]
	
	if err := h.viewer.StopViewer(r.Context(), viewerID); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/metrics - 获取指标数据
func (h *Handlers) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.aggregator.GetMetrics()
	h.writeJSON(w, http.StatusOK, metrics)
}

// 健康检查
func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
		"version":   "v1.0.0", // 应该从build信息获取
	}
	h.writeJSON(w, http.StatusOK, health)
}

// 静态文件处理
func (h *Handlers) HandleStatic() http.Handler {
	// 在实际实现中，这里应该处理嵌入的静态文件
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.writeError(w, http.StatusNotFound, "Static files not implemented")
	})
}