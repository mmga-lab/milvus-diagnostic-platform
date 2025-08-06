// API 客户端类
class DashboardAPI {
    constructor(baseURL = '') {
        this.baseURL = baseURL;
    }

    // 通用HTTP请求方法
    async request(endpoint, options = {}) {
        const url = `${this.baseURL}${endpoint}`;
        const config = {
            headers: {
                'Content-Type': 'application/json',
                ...options.headers
            },
            ...options
        };

        try {
            const response = await fetch(url, config);
            
            if (!response.ok) {
                const errorData = await response.json().catch(() => ({}));
                throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
            }

            // 检查是否有内容返回
            const contentLength = response.headers.get('content-length');
            if (contentLength === '0' || response.status === 204) {
                return null;
            }

            return await response.json();
        } catch (error) {
            console.error(`API request failed: ${endpoint}`, error);
            throw error;
        }
    }

    // GET 请求
    async get(endpoint, params = {}) {
        const query = new URLSearchParams(params).toString();
        const url = query ? `${endpoint}?${query}` : endpoint;
        return this.request(url, { method: 'GET' });
    }

    // POST 请求
    async post(endpoint, data = {}) {
        return this.request(endpoint, {
            method: 'POST',
            body: JSON.stringify(data)
        });
    }

    // PUT 请求
    async put(endpoint, data = {}) {
        return this.request(endpoint, {
            method: 'PUT',
            body: JSON.stringify(data)
        });
    }

    // DELETE 请求
    async delete(endpoint) {
        return this.request(endpoint, { method: 'DELETE' });
    }

    // Dashboard API 方法
    
    // 获取总览数据
    async getSummary() {
        return this.get('/api/v1/summary');
    }

    // 获取实例列表
    async getInstances(params = {}) {
        const defaultParams = {
            page: 1,
            pageSize: 20,
            sortBy: 'name',
            sortDesc: false
        };
        return this.get('/api/v1/instances', { ...defaultParams, ...params });
    }

    // 获取实例的Pod详情
    async getInstancePods(instanceName, namespace = 'default') {
        return this.get(`/api/v1/instances/${encodeURIComponent(instanceName)}/pods`, { namespace });
    }

    // 获取Coredump列表
    async getCoredumps(params = {}) {
        const defaultParams = {
            page: 1,
            pageSize: 20,
            sortBy: 'time',
            sortDesc: true
        };
        return this.get('/api/v1/coredumps', { ...defaultParams, ...params });
    }

    // 获取单个Coredump详情
    async getCoredumpDetail(coredumpId) {
        return this.get(`/api/v1/coredumps/${encodeURIComponent(coredumpId)}`);
    }

    // 创建Coredump查看器
    async createViewer(coredumpId, duration = 30) {
        return this.post(`/api/v1/coredumps/${encodeURIComponent(coredumpId)}/view`, { 
            coredumpId, 
            duration 
        });
    }

    // 获取查看器状态
    async getViewerStatus(viewerId) {
        return this.get(`/api/v1/viewers/${encodeURIComponent(viewerId)}`);
    }

    // 停止查看器
    async stopViewer(viewerId) {
        return this.delete(`/api/v1/viewers/${encodeURIComponent(viewerId)}`);
    }

    // 获取指标数据
    async getMetrics() {
        return this.get('/api/v1/metrics');
    }

    // 健康检查
    async getHealth() {
        return this.get('/api/v1/health');
    }
}

// 创建全局API实例
const api = new DashboardAPI();

// 工具函数

// 格式化文件大小
function formatFileSize(bytes) {
    if (bytes === 0) return '0 B';
    
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

// 格式化时间
function formatTime(dateString) {
    if (!dateString) return '-';
    
    const date = new Date(dateString);
    const now = new Date();
    const diff = now - date;
    
    // 小于1分钟
    if (diff < 60000) {
        return '刚刚';
    }
    
    // 小于1小时
    if (diff < 3600000) {
        const minutes = Math.floor(diff / 60000);
        return `${minutes}分钟前`;
    }
    
    // 小于24小时
    if (diff < 86400000) {
        const hours = Math.floor(diff / 3600000);
        return `${hours}小时前`;
    }
    
    // 超过24小时，显示具体日期
    return date.toLocaleDateString('zh-CN') + ' ' + date.toLocaleTimeString('zh-CN', { 
        hour: '2-digit', 
        minute: '2-digit' 
    });
}

// 格式化相对时间
function formatRelativeTime(dateString) {
    if (!dateString) return '-';
    
    const date = new Date(dateString);
    const now = new Date();
    const diff = now - date;
    
    if (diff < 1000) return '现在';
    if (diff < 60000) return `${Math.floor(diff / 1000)}秒前`;
    if (diff < 3600000) return `${Math.floor(diff / 60000)}分钟前`;
    if (diff < 86400000) return `${Math.floor(diff / 3600000)}小时前`;
    if (diff < 604800000) return `${Math.floor(diff / 86400000)}天前`;
    
    return date.toLocaleDateString('zh-CN');
}

// 获取状态标签HTML
function getStatusBadge(status) {
    const statusConfig = {
        'running': { class: 'status-running', text: '运行中', icon: 'fas fa-play' },
        'failed': { class: 'status-failed', text: '失败', icon: 'fas fa-times' },
        'pending': { class: 'status-pending', text: '等待中', icon: 'fas fa-clock' },
        'terminating': { class: 'status-terminating', text: '终止中', icon: 'fas fa-stop' },
        'discovered': { class: 'status-pending', text: '已发现', icon: 'fas fa-search' },
        'processing': { class: 'status-pending', text: '处理中', icon: 'fas fa-cog' },
        'analyzed': { class: 'status-running', text: '已分析', icon: 'fas fa-check' },
        'stored': { class: 'status-running', text: '已存储', icon: 'fas fa-save' },
        'skipped': { class: 'status-terminating', text: '已跳过', icon: 'fas fa-forward' },
        'error': { class: 'status-failed', text: '错误', icon: 'fas fa-exclamation' }
    };
    
    const config = statusConfig[status] || { class: 'status-terminating', text: status, icon: 'fas fa-question' };
    
    return `<span class="badge status-badge ${config.class}">
                <i class="${config.icon} me-1"></i>${config.text}
            </span>`;
}

// 获取评分显示HTML
function getScoreDisplay(score) {
    let scoreClass = 'score-low';
    let stars = '';
    
    if (score >= 7) {
        scoreClass = 'score-high';
        stars = '<span class="score-stars">★★★</span>';
    } else if (score >= 4) {
        scoreClass = 'score-medium';
        stars = '<span class="score-stars">★★</span>';
    } else {
        stars = '<span class="score-stars">★</span>';
    }
    
    return `<span class="score-display ${scoreClass}">
                ${score.toFixed(1)}
                ${stars}
            </span>`;
}

// 获取部署类型显示
function getDeploymentTypeDisplay(type) {
    const typeConfig = {
        'helm': { icon: 'fas fa-ship', text: 'Helm', class: 'text-primary' },
        'operator': { icon: 'fas fa-robot', text: 'Operator', class: 'text-info' }
    };
    
    const config = typeConfig[type] || { icon: 'fas fa-question', text: type, class: 'text-secondary' };
    
    return `<span class="${config.class}">
                <i class="${config.icon} me-1"></i>${config.text}
            </span>`;
}

// 错误处理显示
function showError(message, container = null) {
    const errorHtml = `
        <div class="alert alert-danger alert-dismissible fade show" role="alert">
            <i class="fas fa-exclamation-triangle me-2"></i>
            <strong>错误:</strong> ${message}
            <button type="button" class="btn-close" data-bs-dismiss="alert"></button>
        </div>
    `;
    
    if (container) {
        container.innerHTML = errorHtml;
    } else {
        // 在页面顶部显示错误
        const alertContainer = document.getElementById('alert-container') || document.body;
        alertContainer.insertAdjacentHTML('afterbegin', errorHtml);
    }
}

// 加载状态显示
function showLoading(container, message = '加载中...') {
    const loadingHtml = `
        <div class="loading">
            <i class="fas fa-spinner fa-spin fa-2x mb-3"></i>
            <p>${message}</p>
        </div>
    `;
    
    container.innerHTML = loadingHtml;
}

// 空状态显示
function showEmptyState(container, message = '暂无数据', icon = 'fas fa-inbox') {
    const emptyHtml = `
        <div class="empty-state">
            <i class="${icon}"></i>
            <p>${message}</p>
        </div>
    `;
    
    container.innerHTML = emptyHtml;
}

// 分页组件生成
function generatePagination(containerId, currentPage, totalPages, onPageChange) {
    const container = document.getElementById(containerId);
    if (!container || totalPages <= 1) {
        container.innerHTML = '';
        return;
    }
    
    let paginationHtml = '';
    
    // 上一页按钮
    if (currentPage > 1) {
        paginationHtml += `
            <li class="page-item">
                <a class="page-link" href="#" onclick="${onPageChange}(${currentPage - 1}); return false;">
                    <i class="fas fa-chevron-left"></i>
                </a>
            </li>
        `;
    }
    
    // 页码按钮
    const startPage = Math.max(1, currentPage - 2);
    const endPage = Math.min(totalPages, currentPage + 2);
    
    if (startPage > 1) {
        paginationHtml += `
            <li class="page-item">
                <a class="page-link" href="#" onclick="${onPageChange}(1); return false;">1</a>
            </li>
        `;
        if (startPage > 2) {
            paginationHtml += '<li class="page-item disabled"><span class="page-link">...</span></li>';
        }
    }
    
    for (let i = startPage; i <= endPage; i++) {
        const activeClass = i === currentPage ? 'active' : '';
        paginationHtml += `
            <li class="page-item ${activeClass}">
                <a class="page-link" href="#" onclick="${onPageChange}(${i}); return false;">${i}</a>
            </li>
        `;
    }
    
    if (endPage < totalPages) {
        if (endPage < totalPages - 1) {
            paginationHtml += '<li class="page-item disabled"><span class="page-link">...</span></li>';
        }
        paginationHtml += `
            <li class="page-item">
                <a class="page-link" href="#" onclick="${onPageChange}(${totalPages}); return false;">${totalPages}</a>
            </li>
        `;
    }
    
    // 下一页按钮
    if (currentPage < totalPages) {
        paginationHtml += `
            <li class="page-item">
                <a class="page-link" href="#" onclick="${onPageChange}(${currentPage + 1}); return false;">
                    <i class="fas fa-chevron-right"></i>
                </a>
            </li>
        `;
    }
    
    container.innerHTML = paginationHtml;
}

// URL编码工具
function encodeCoredumpId(path) {
    return encodeURIComponent(btoa(path));
}

function decodeCoredumpId(encodedId) {
    return atob(decodeURIComponent(encodedId));
}