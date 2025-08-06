// 主应用逻辑
class DashboardApp {
    constructor() {
        this.currentSection = 'dashboard';
        this.currentInstancesPage = 1;
        this.currentCoredumpsPage = 1;
        this.refreshInterval = null;
        this.viewerTimers = new Map(); // 存储查看器定时器
        
        this.init();
    }

    async init() {
        // 初始化导航
        this.initNavigation();
        
        // 初始化页面
        this.showSection('dashboard');
        
        // 加载初始数据
        await this.loadDashboardData();
        
        // 开始自动刷新
        this.startAutoRefresh();
        
        // 初始化事件监听
        this.initEventListeners();
    }

    initNavigation() {
        const navLinks = document.querySelectorAll('.navbar-nav .nav-link');
        navLinks.forEach(link => {
            link.addEventListener('click', (e) => {
                e.preventDefault();
                const href = link.getAttribute('href');
                if (href && href.startsWith('#')) {
                    const section = href.substring(1);
                    this.showSection(section);
                }
            });
        });
    }

    initEventListeners() {
        // Coredump排序选择器
        const sortSelect = document.getElementById('coredump-sort-select');
        if (sortSelect) {
            sortSelect.addEventListener('change', () => {
                this.currentCoredumpsPage = 1;
                this.loadCoredumps();
            });
        }
        
        // 模态框事件
        const coredumpModal = document.getElementById('coredumpDetailModal');
        if (coredumpModal) {
            coredumpModal.addEventListener('hidden.bs.modal', () => {
                document.getElementById('coredump-detail-content').innerHTML = '';
            });
        }
    }

    showSection(sectionName) {
        // 更新导航状态
        document.querySelectorAll('.navbar-nav .nav-link').forEach(link => {
            link.classList.remove('active');
        });
        document.querySelector(`a[href="#${sectionName}"]`)?.classList.add('active');

        // 显示对应的内容区域
        document.querySelectorAll('.dashboard-section').forEach(section => {
            section.classList.add('d-none');
        });
        
        const targetSection = document.getElementById(`${sectionName}-section`);
        if (targetSection) {
            targetSection.classList.remove('d-none');
        }

        this.currentSection = sectionName;
        
        // 根据当前页面加载数据
        this.loadSectionData(sectionName);
    }

    async loadSectionData(sectionName) {
        try {
            switch (sectionName) {
                case 'dashboard':
                    await this.loadDashboardData();
                    break;
                case 'instances':
                    await this.loadInstances();
                    break;
                case 'coredumps':
                    await this.loadCoredumps();
                    break;
            }
        } catch (error) {
            console.error(`Failed to load ${sectionName} data:`, error);
            showError(`加载${sectionName}数据失败: ${error.message}`);
        }
    }

    async loadDashboardData() {
        try {
            // 加载总览数据
            const summary = await api.getSummary();
            this.updateSummaryCards(summary);
            
            // 加载指标数据
            const metrics = await api.getMetrics();
            this.updateCharts(metrics);
            
            // 加载最近活动
            await this.loadRecentActivities();
            
        } catch (error) {
            console.error('Failed to load dashboard data:', error);
            showError('加载Dashboard数据失败: ' + error.message);
        }
    }

    updateSummaryCards(summary) {
        document.getElementById('instances-count').textContent = summary.milvusInstances || 0;
        document.getElementById('coredumps-count').textContent = summary.totalCoredumps || 0;
        document.getElementById('processed-today').textContent = summary.processedToday || 0;
        document.getElementById('high-value-count').textContent = summary.highValueCoredumps || 0;
        
        // 更新Agent状态
        const statusElement = document.getElementById('agent-status');
        if (summary.agentStatus === 'running') {
            statusElement.textContent = '运行中';
            statusElement.previousElementSibling.className = 'fas fa-circle text-success me-1';
        } else {
            statusElement.textContent = '离线';
            statusElement.previousElementSibling.className = 'fas fa-circle text-danger me-1';
        }
    }

    updateCharts(metrics) {
        if (dashboardCharts) {
            // 更新处理趋势图
            if (metrics.processingTrends) {
                dashboardCharts.updateProcessingTrend(metrics.processingTrends);
            }
            
            // 更新评分分布图
            if (metrics.scoreDistribution) {
                dashboardCharts.updateScoreDistribution(metrics.scoreDistribution);
            }
        }
    }

    async loadRecentActivities() {
        const container = document.getElementById('recent-activities');
        if (!container) return;

        try {
            // 获取最近的coredumps作为活动
            const result = await api.getCoredumps({ 
                page: 1, 
                pageSize: 5, 
                sortBy: 'time', 
                sortDesc: true 
            });

            if (result.data && result.data.length > 0) {
                const activitiesHtml = result.data.map(coredump => {
                    const activity = this.createActivityItem(coredump);
                    return activity;
                }).join('');
                
                container.innerHTML = activitiesHtml;
            } else {
                showEmptyState(container, '暂无最近活动', 'fas fa-clock');
            }
        } catch (error) {
            console.error('Failed to load recent activities:', error);
            showError('加载最近活动失败: ' + error.message, container);
        }
    }

    createActivityItem(coredump) {
        const timeAgo = formatRelativeTime(coredump.file.modTime);
        const scoreClass = coredump.valueScore >= 7 ? 'text-success' : 
                          coredump.valueScore >= 4 ? 'text-warning' : 'text-danger';
        
        return `
            <div class="activity-item">
                <div class="time">${timeAgo}</div>
                <div class="message">
                    发现新的Coredump文件: <strong>${coredump.file.fileName}</strong>
                </div>
                <div class="details">
                    实例: ${coredump.instance} | 
                    评分: <span class="${scoreClass}">${coredump.valueScore.toFixed(1)}</span> |
                    大小: ${formatFileSize(coredump.file.size)}
                </div>
            </div>
        `;
    }

    async loadInstances() {
        const tableBody = document.getElementById('instances-table-body');
        if (!tableBody) return;

        showLoading(tableBody, '正在加载实例数据...');

        try {
            const result = await api.getInstances({
                page: this.currentInstancesPage,
                pageSize: 20,
                sortBy: 'coredumps',
                sortDesc: true
            });

            if (result.data && result.data.length > 0) {
                const rowsHtml = result.data.map(instance => this.createInstanceRow(instance)).join('');
                tableBody.innerHTML = rowsHtml;
                
                // 生成分页
                generatePagination('instances-pagination', result.page, result.totalPages, 'loadInstancesPage');
            } else {
                showEmptyState(tableBody, '暂无发现的Milvus实例', 'fas fa-server');
            }
        } catch (error) {
            console.error('Failed to load instances:', error);
            showError('加载实例数据失败: ' + error.message, tableBody);
        }
    }

    createInstanceRow(instance) {
        const statusBadge = getStatusBadge(instance.status);
        const typeBadge = getDeploymentTypeDisplay(instance.instance.type);
        const lastActivity = instance.lastActivity ? formatTime(instance.lastActivity) : '-';
        
        return `
            <tr>
                <td><strong>${instance.instance.name}</strong></td>
                <td>${instance.instance.namespace}</td>
                <td>${typeBadge}</td>
                <td>${statusBadge}</td>
                <td>
                    <span class="badge bg-info">${instance.podCount}</span>
                </td>
                <td>
                    <span class="badge ${instance.coredumpCount > 0 ? 'bg-warning' : 'bg-light text-dark'}">${instance.coredumpCount}</span>
                </td>
                <td>
                    <span class="time-display">${lastActivity}</span>
                    ${instance.recentRestarts > 0 ? `<br><small class="text-danger">最近${instance.recentRestarts}次重启</small>` : ''}
                </td>
                <td>
                    <div class="action-buttons">
                        <button class="btn btn-sm btn-outline-primary" onclick="viewInstancePods('${instance.instance.name}', '${instance.instance.namespace}')">
                            <i class="fas fa-eye"></i> 查看Pod
                        </button>
                        ${instance.coredumpCount > 0 ? `
                            <button class="btn btn-sm btn-outline-info" onclick="filterCoredumpsByInstance('${instance.instance.name}')">
                                <i class="fas fa-file-medical"></i> 查看Coredumps
                            </button>
                        ` : ''}
                    </div>
                </td>
            </tr>
        `;
    }

    async loadCoredumps() {
        const tableBody = document.getElementById('coredumps-table-body');
        const sortSelect = document.getElementById('coredump-sort-select');
        if (!tableBody) return;

        showLoading(tableBody, '正在加载Coredump数据...');

        try {
            const sortBy = sortSelect ? sortSelect.value : 'time';
            const result = await api.getCoredumps({
                page: this.currentCoredumpsPage,
                pageSize: 20,
                sortBy: sortBy,
                sortDesc: true
            });

            if (result.data && result.data.length > 0) {
                const rowsHtml = result.data.map(coredump => this.createCoredumpRow(coredump)).join('');
                tableBody.innerHTML = rowsHtml;
                
                // 生成分页
                generatePagination('coredumps-pagination', result.page, result.totalPages, 'loadCoredumpsPage');
            } else {
                showEmptyState(tableBody, '暂无Coredump文件', 'fas fa-file-medical');
            }
        } catch (error) {
            console.error('Failed to load coredumps:', error);
            showError('加载Coredump数据失败: ' + error.message, tableBody);
        }
    }

    createCoredumpRow(coredump) {
        const scoreDisplay = getScoreDisplay(coredump.valueScore);
        const statusBadge = getStatusBadge(coredump.storageStatus);
        const timeDisplay = formatTime(coredump.file.modTime);
        const sizeDisplay = formatFileSize(coredump.file.size);
        const aiAnalysisBadge = coredump.hasAiAnalysis ? '<span class="ai-badge">AI</span>' : '';
        const coredumpId = encodeCoredumpId(coredump.file.path);
        
        return `
            <tr>
                <td>
                    <strong>${coredump.file.fileName}</strong>
                    <br><small class="text-muted">${coredump.file.path}</small>
                </td>
                <td>
                    <span class="badge bg-secondary">${coredump.namespace}</span>
                    <br>${coredump.instance}
                </td>
                <td>
                    <span class="file-size">${sizeDisplay}</span>
                </td>
                <td>
                    <span class="time-display">${timeDisplay}</span>
                </td>
                <td>${scoreDisplay}</td>
                <td>${statusBadge}</td>
                <td>${aiAnalysisBadge}</td>
                <td>
                    <div class="action-buttons">
                        <button class="btn btn-sm btn-outline-info" onclick="viewCoredumpDetail('${coredumpId}')">
                            <i class="fas fa-info-circle"></i> 详情
                        </button>
                        ${coredump.canView ? `
                            <button class="btn btn-sm btn-outline-primary" onclick="launchViewer('${coredumpId}')">
                                <i class="fas fa-eye"></i> 查看
                            </button>
                        ` : ''}
                    </div>
                </td>
            </tr>
        `;
    }

    startAutoRefresh() {
        // 停止现有的刷新定时器
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
        }
        
        // 设置自动刷新，每30秒更新一次
        this.refreshInterval = setInterval(() => {
            this.loadSectionData(this.currentSection);
        }, 30000);
    }

    stopAutoRefresh() {
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
            this.refreshInterval = null;
        }
    }

    // 手动刷新方法
    async refreshCurrentSection() {
        await this.loadSectionData(this.currentSection);
    }
}

// 全局函数供HTML调用

function loadInstancesPage(page) {
    app.currentInstancesPage = page;
    app.loadInstances();
}

function loadCoredumpsPage(page) {
    app.currentCoredumpsPage = page;
    app.loadCoredumps();
}

function refreshInstances() {
    app.loadInstances();
}

function refreshCoredumps() {
    app.loadCoredumps();
}

async function viewInstancePods(instanceName, namespace) {
    try {
        const pods = await api.getInstancePods(instanceName, namespace);
        
        // 显示Pod详情模态框
        showInstancePodsModal(instanceName, namespace, pods);
    } catch (error) {
        showError(`获取实例Pod信息失败: ${error.message}`);
    }
}

function showInstancePodsModal(instanceName, namespace, pods) {
    const modalHtml = `
        <div class="modal fade" id="instancePodsModal" tabindex="-1">
            <div class="modal-dialog modal-lg">
                <div class="modal-content">
                    <div class="modal-header">
                        <h5 class="modal-title">实例Pod详情: ${instanceName}</h5>
                        <button type="button" class="btn-close" data-bs-dismiss="modal"></button>
                    </div>
                    <div class="modal-body">
                        ${createPodsTable(pods)}
                    </div>
                    <div class="modal-footer">
                        <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">关闭</button>
                    </div>
                </div>
            </div>
        </div>
    `;
    
    // 移除已存在的模态框
    const existingModal = document.getElementById('instancePodsModal');
    if (existingModal) {
        existingModal.remove();
    }
    
    document.body.insertAdjacentHTML('beforeend', modalHtml);
    const modal = new bootstrap.Modal(document.getElementById('instancePodsModal'));
    modal.show();
}

function createPodsTable(pods) {
    if (!pods || pods.length === 0) {
        return '<p class="text-muted">此实例暂无Pod信息</p>';
    }
    
    const rowsHtml = pods.map(pod => {
        const statusBadge = getStatusBadge(pod.status.toLowerCase());
        const timeDisplay = formatTime(pod.lastRestart);
        
        return `
            <tr>
                <td><strong>${pod.name}</strong></td>
                <td>${statusBadge}</td>
                <td><span class="badge bg-warning">${pod.restartCount}</span></td>
                <td><span class="time-display">${timeDisplay}</span></td>
                <td>
                    ${pod.containerStatuses.map(container => `
                        <div class="mb-1">
                            <span class="badge ${container.ready ? 'bg-success' : 'bg-danger'}">${container.name}</span>
                            ${container.lastTerminationReason ? `<small class="text-muted d-block">${container.lastTerminationReason}</small>` : ''}
                        </div>
                    `).join('')}
                </td>
            </tr>
        `;
    }).join('');
    
    return `
        <div class="table-responsive">
            <table class="table table-striped">
                <thead>
                    <tr>
                        <th>Pod名称</th>
                        <th>状态</th>
                        <th>重启次数</th>
                        <th>最后重启</th>
                        <th>容器状态</th>
                    </tr>
                </thead>
                <tbody>
                    ${rowsHtml}
                </tbody>
            </table>
        </div>
    `;
}

function filterCoredumpsByInstance(instanceName) {
    // 切换到coredumps页面并应用过滤器
    app.showSection('coredumps');
    // TODO: 实现实例过滤功能
}

async function viewCoredumpDetail(encodedCoredumpId) {
    try {
        const coredumpId = decodeCoredumpId(encodedCoredumpId);
        const detail = await api.getCoredumpDetail(coredumpId);
        
        showCoredumpDetailModal(detail, encodedCoredumpId);
    } catch (error) {
        showError(`获取Coredump详情失败: ${error.message}`);
    }
}

function showCoredumpDetailModal(detail, encodedCoredumpId) {
    const content = document.getElementById('coredump-detail-content');
    if (!content) return;
    
    content.innerHTML = createCoredumpDetailContent(detail);
    
    // 设置查看器按钮
    const viewerBtn = document.getElementById('launch-viewer-btn');
    if (viewerBtn) {
        viewerBtn.onclick = () => launchViewer(encodedCoredumpId);
        viewerBtn.style.display = detail.file.status === 'stored' ? 'inline-block' : 'none';
    }
    
    const modal = new bootstrap.Modal(document.getElementById('coredumpDetailModal'));
    modal.show();
}

function createCoredumpDetailContent(detail) {
    const scoreBreakdown = detail.scoreBreakdown ? createScoreBreakdownHtml(detail.scoreBreakdown) : '';
    const gdbOutput = detail.gdbOutput ? `
        <div class="mt-3">
            <h6>GDB分析结果</h6>
            <div class="code-block">${detail.gdbOutput}</div>
        </div>
    ` : '';
    
    const aiAnalysis = detail.file.analysisResults && detail.file.analysisResults.aiAnalysis ? 
        createAIAnalysisHtml(detail.file.analysisResults.aiAnalysis) : '';
    
    return `
        <div class="row">
            <div class="col-md-6">
                <div class="detail-panel">
                    <h6>基本信息</h6>
                    <div class="detail-item">
                        <span class="label">文件名:</span>
                        <span class="value">${detail.file.fileName}</span>
                    </div>
                    <div class="detail-item">
                        <span class="label">路径:</span>
                        <span class="value">${detail.file.path}</span>
                    </div>
                    <div class="detail-item">
                        <span class="label">大小:</span>
                        <span class="value">${formatFileSize(detail.file.size)}</span>
                    </div>
                    <div class="detail-item">
                        <span class="label">创建时间:</span>
                        <span class="value">${formatTime(detail.file.modTime)}</span>
                    </div>
                    <div class="detail-item">
                        <span class="label">信号:</span>
                        <span class="value">${detail.file.signal}</span>
                    </div>
                    <div class="detail-item">
                        <span class="label">PID:</span>
                        <span class="value">${detail.file.pid}</span>
                    </div>
                </div>
            </div>
            <div class="col-md-6">
                <div class="detail-panel">
                    <h6>关联信息</h6>
                    <div class="detail-item">
                        <span class="label">实例:</span>
                        <span class="value">${detail.file.instanceName || '-'}</span>
                    </div>
                    <div class="detail-item">
                        <span class="label">Pod:</span>
                        <span class="value">${detail.file.podName || '-'}</span>
                    </div>
                    <div class="detail-item">
                        <span class="label">容器:</span>
                        <span class="value">${detail.file.containerName || '-'}</span>
                    </div>
                    <div class="detail-item">
                        <span class="label">命名空间:</span>
                        <span class="value">${detail.file.podNamespace || '-'}</span>
                    </div>
                    <div class="detail-item">
                        <span class="label">状态:</span>
                        <span class="value">${getStatusBadge(detail.file.status)}</span>
                    </div>
                </div>
            </div>
        </div>
        
        ${scoreBreakdown}
        ${gdbOutput}
        ${aiAnalysis}
    `;
}

function createScoreBreakdownHtml(breakdown) {
    return `
        <div class="mt-3">
            <h6>价值评分详情</h6>
            <div class="score-breakdown">
                <div class="score-item">
                    <span>基础分</span>
                    <span>${breakdown.baseScore.toFixed(1)}</span>
                </div>
                <div class="score-item">
                    <span>崩溃原因</span>
                    <span>+${breakdown.crashReasonScore.toFixed(1)}</span>
                </div>
                <div class="score-item">
                    <span>Panic关键词</span>
                    <span>+${breakdown.panicKeywordScore.toFixed(1)}</span>
                </div>
                <div class="score-item">
                    <span>栈跟踪质量</span>
                    <span>+${breakdown.stackTraceScore.toFixed(1)}</span>
                </div>
                <div class="score-item">
                    <span>线程复杂度</span>
                    <span>+${breakdown.threadScore.toFixed(1)}</span>
                </div>
                <div class="score-item">
                    <span>Pod关联</span>
                    <span>+${breakdown.podAssocScore.toFixed(1)}</span>
                </div>
                <div class="score-item">
                    <span>信号严重性</span>
                    <span>+${breakdown.signalScore.toFixed(1)}</span>
                </div>
                <div class="score-item">
                    <span>文件大小</span>
                    <span>+${breakdown.fileSizeScore.toFixed(1)}</span>
                </div>
                <div class="score-item">
                    <span>时效性</span>
                    <span>+${breakdown.freshnessScore.toFixed(1)}</span>
                </div>
                <div class="score-item">
                    <span><strong>总分</strong></span>
                    <span><strong>${breakdown.totalScore.toFixed(1)}</strong></span>
                </div>
            </div>
        </div>
    `;
}

function createAIAnalysisHtml(aiAnalysis) {
    const recommendations = aiAnalysis.recommendations && aiAnalysis.recommendations.length > 0 ?
        `<ul>${aiAnalysis.recommendations.map(rec => `<li>${rec}</li>`).join('')}</ul>` : '暂无建议';
    
    const codeSuggestions = aiAnalysis.codeSuggestions && aiAnalysis.codeSuggestions.length > 0 ?
        aiAnalysis.codeSuggestions.map(suggestion => `
            <div class="card mt-2">
                <div class="card-body p-2">
                    <h6 class="card-title mb-1">${suggestion.file}:${suggestion.function}()</h6>
                    <p class="card-text mb-1"><small class="text-muted">${suggestion.issue}</small></p>
                    <code>${suggestion.suggestion}</code>
                    <span class="badge bg-${suggestion.priority === 'high' ? 'danger' : suggestion.priority === 'medium' ? 'warning' : 'info'} ms-2">
                        ${suggestion.priority}
                    </span>
                </div>
            </div>
        `).join('') : '暂无代码建议';
    
    return `
        <div class="mt-3">
            <h6><i class="fas fa-robot me-2"></i>AI智能分析</h6>
            <div class="row">
                <div class="col-md-12">
                    <div class="detail-panel">
                        <div class="detail-item">
                            <span class="label">摘要:</span>
                            <span class="value">${aiAnalysis.summary || '-'}</span>
                        </div>
                        <div class="detail-item">
                            <span class="label">根本原因:</span>
                            <span class="value">${aiAnalysis.rootCause || '-'}</span>
                        </div>
                        <div class="detail-item">
                            <span class="label">影响评估:</span>
                            <span class="value">${aiAnalysis.impact || '-'}</span>
                        </div>
                        <div class="detail-item">
                            <span class="label">置信度:</span>
                            <span class="value">${(aiAnalysis.confidence * 100).toFixed(1)}%</span>
                        </div>
                        <div class="detail-item">
                            <span class="label">成本:</span>
                            <span class="value">$${aiAnalysis.costUsd ? aiAnalysis.costUsd.toFixed(4) : '0.0000'}</span>
                        </div>
                    </div>
                </div>
                <div class="col-md-6">
                    <h6>修复建议</h6>
                    ${recommendations}
                </div>
                <div class="col-md-6">
                    <h6>代码建议</h6>
                    ${codeSuggestions}
                </div>
            </div>
        </div>
    `;
}

async function launchViewer(encodedCoredumpId) {
    try {
        const response = await api.createViewer(decodeCoredumpId(encodedCoredumpId), 30);
        
        // 显示查看器信息
        showViewerModal(response);
    } catch (error) {
        showError(`启动Coredump查看器失败: ${error.message}`);
    }
}

function showViewerModal(viewerResponse) {
    const modalHtml = `
        <div class="modal fade" id="viewerModal" tabindex="-1">
            <div class="modal-dialog">
                <div class="modal-content">
                    <div class="modal-header">
                        <h5 class="modal-title">Coredump查看器</h5>
                        <button type="button" class="btn-close" data-bs-dismiss="modal"></button>
                    </div>
                    <div class="modal-body">
                        <div class="alert alert-success">
                            <i class="fas fa-check-circle me-2"></i>
                            查看器已成功启动！
                        </div>
                        <div class="detail-panel">
                            <div class="detail-item">
                                <span class="label">查看器ID:</span>
                                <span class="value">${viewerResponse.viewerId}</span>
                            </div>
                            <div class="detail-item">
                                <span class="label">Pod名称:</span>
                                <span class="value">${viewerResponse.podName}</span>
                            </div>
                            <div class="detail-item">
                                <span class="label">过期时间:</span>
                                <span class="value">${formatTime(viewerResponse.expiresAt)}</span>
                            </div>
                            <div class="detail-item">
                                <span class="label">状态:</span>
                                <span class="value">${getStatusBadge(viewerResponse.status)}</span>
                            </div>
                        </div>
                        ${viewerResponse.webTermUrl ? `
                            <div class="mt-3">
                                <a href="${viewerResponse.webTermUrl}" target="_blank" class="btn btn-primary">
                                    <i class="fas fa-external-link-alt me-2"></i>打开Web终端
                                </a>
                            </div>
                        ` : ''}
                    </div>
                    <div class="modal-footer">
                        <button type="button" class="btn btn-secondary" data-bs-dismiss="modal">关闭</button>
                        <button type="button" class="btn btn-danger" onclick="stopViewerFromModal('${viewerResponse.viewerId}')">
                            <i class="fas fa-stop"></i> 停止查看器
                        </button>
                    </div>
                </div>
            </div>
        </div>
    `;
    
    // 移除已存在的模态框
    const existingModal = document.getElementById('viewerModal');
    if (existingModal) {
        existingModal.remove();
    }
    
    document.body.insertAdjacentHTML('beforeend', modalHtml);
    const modal = new bootstrap.Modal(document.getElementById('viewerModal'));
    modal.show();
}

async function stopViewerFromModal(viewerId) {
    try {
        await api.stopViewer(viewerId);
        
        // 关闭模态框
        const modal = bootstrap.Modal.getInstance(document.getElementById('viewerModal'));
        if (modal) {
            modal.hide();
        }
        
        // 显示成功消息
        showSuccess('查看器已停止');
    } catch (error) {
        showError(`停止查看器失败: ${error.message}`);
    }
}

function showSuccess(message) {
    const successHtml = `
        <div class="alert alert-success alert-dismissible fade show" role="alert">
            <i class="fas fa-check-circle me-2"></i>
            ${message}
            <button type="button" class="btn-close" data-bs-dismiss="alert"></button>
        </div>
    `;
    
    document.body.insertAdjacentHTML('afterbegin', successHtml);
    
    // 3秒后自动消失
    setTimeout(() => {
        const alert = document.querySelector('.alert-success');
        if (alert) {
            alert.remove();
        }
    }, 3000);
}

// 页面加载完成后初始化应用
let app;
document.addEventListener('DOMContentLoaded', () => {
    app = new DashboardApp();
});