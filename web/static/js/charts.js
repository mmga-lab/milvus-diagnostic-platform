// 图表管理类
class DashboardCharts {
    constructor() {
        this.charts = {};
        this.chartColors = {
            primary: '#007bff',
            secondary: '#6c757d',
            success: '#28a745',
            danger: '#dc3545',
            warning: '#ffc107',
            info: '#17a2b8',
            light: '#f8f9fa',
            dark: '#343a40'
        };
        
        // Chart.js 默认配置
        Chart.defaults.font.family = "'Segoe UI', Tahoma, Geneva, Verdana, sans-serif";
        Chart.defaults.color = this.chartColors.dark;
    }

    // 初始化所有图表
    initCharts() {
        this.initProcessingTrendChart();
        this.initScoreDistributionChart();
    }

    // 处理趋势图表
    initProcessingTrendChart() {
        const ctx = document.getElementById('processing-trend-chart');
        if (!ctx) return;

        this.charts.processingTrend = new Chart(ctx, {
            type: 'line',
            data: {
                labels: [],
                datasets: [{
                    label: 'Coredump处理数量',
                    data: [],
                    borderColor: this.chartColors.primary,
                    backgroundColor: this.chartColors.primary + '20',
                    borderWidth: 2,
                    fill: true,
                    tension: 0.4,
                    pointBackgroundColor: this.chartColors.primary,
                    pointBorderColor: '#fff',
                    pointBorderWidth: 2,
                    pointRadius: 4,
                    pointHoverRadius: 6
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                scales: {
                    y: {
                        beginAtZero: true,
                        ticks: {
                            stepSize: 1
                        },
                        grid: {
                            color: '#e9ecef'
                        }
                    },
                    x: {
                        grid: {
                            color: '#e9ecef'
                        }
                    }
                },
                plugins: {
                    legend: {
                        display: false
                    },
                    tooltip: {
                        backgroundColor: 'rgba(0, 0, 0, 0.8)',
                        titleColor: '#fff',
                        bodyColor: '#fff',
                        borderColor: this.chartColors.primary,
                        borderWidth: 1,
                        cornerRadius: 6,
                        displayColors: false,
                        callbacks: {
                            title: function(context) {
                                return `时间: ${context[0].label}`;
                            },
                            label: function(context) {
                                return `处理数量: ${context.parsed.y}`;
                            }
                        }
                    }
                },
                interaction: {
                    intersect: false,
                    mode: 'index'
                }
            }
        });
    }

    // 评分分布饼图
    initScoreDistributionChart() {
        const ctx = document.getElementById('score-distribution-chart');
        if (!ctx) return;

        this.charts.scoreDistribution = new Chart(ctx, {
            type: 'doughnut',
            data: {
                labels: ['低价值 (0-4)', '中等 (4-6)', '高价值 (6-8)', '极高 (8-10)'],
                datasets: [{
                    data: [],
                    backgroundColor: [
                        this.chartColors.danger,
                        this.chartColors.warning,
                        this.chartColors.success,
                        this.chartColors.primary
                    ],
                    borderColor: '#fff',
                    borderWidth: 2,
                    hoverBorderWidth: 3
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                plugins: {
                    legend: {
                        position: 'bottom',
                        labels: {
                            padding: 15,
                            usePointStyle: true,
                            font: {
                                size: 12
                            }
                        }
                    },
                    tooltip: {
                        backgroundColor: 'rgba(0, 0, 0, 0.8)',
                        titleColor: '#fff',
                        bodyColor: '#fff',
                        borderColor: this.chartColors.primary,
                        borderWidth: 1,
                        cornerRadius: 6,
                        callbacks: {
                            label: function(context) {
                                const label = context.label || '';
                                const value = context.parsed;
                                const total = context.dataset.data.reduce((a, b) => a + b, 0);
                                const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : '0';
                                return `${label}: ${value} (${percentage}%)`;
                            }
                        }
                    }
                },
                cutout: '60%'
            }
        });
    }

    // 更新处理趋势数据
    updateProcessingTrend(trendData) {
        const chart = this.charts.processingTrend;
        if (!chart) return;

        const labels = trendData.map(point => point.label);
        const data = trendData.map(point => point.value);

        chart.data.labels = labels;
        chart.data.datasets[0].data = data;
        chart.update('none'); // 不使用动画以提高性能
    }

    // 更新评分分布数据
    updateScoreDistribution(distributionData) {
        const chart = this.charts.scoreDistribution;
        if (!chart) return;

        // 重新组织数据，确保顺序正确
        const scoreMap = {};
        distributionData.forEach(item => {
            scoreMap[item.scoreRange] = item.count;
        });

        const orderedRanges = ['0-2', '2-4', '4-6', '6-8', '8-10'];
        const data = [];
        
        // 合并0-2和2-4为低价值，4-6为中等，6-8为高价值，8-10为极高
        const lowValue = (scoreMap['0-2'] || 0) + (scoreMap['2-4'] || 0);
        const mediumValue = scoreMap['4-6'] || 0;
        const highValue = scoreMap['6-8'] || 0;
        const extremeValue = scoreMap['8-10'] || 0;

        data.push(lowValue, mediumValue, highValue, extremeValue);

        chart.data.datasets[0].data = data;
        chart.update('active');
    }

    // 创建实时更新的小型图表
    createMiniChart(containerId, data, type = 'line') {
        const container = document.getElementById(containerId);
        if (!container) return;

        const canvas = document.createElement('canvas');
        canvas.width = 100;
        canvas.height = 40;
        container.appendChild(canvas);

        const ctx = canvas.getContext('2d');
        
        const miniChart = new Chart(ctx, {
            type: type,
            data: {
                labels: data.labels || [],
                datasets: [{
                    data: data.values || [],
                    borderColor: this.chartColors.primary,
                    backgroundColor: this.chartColors.primary + '30',
                    borderWidth: 1,
                    fill: true,
                    pointRadius: 0,
                    tension: 0.4
                }]
            },
            options: {
                responsive: false,
                maintainAspectRatio: false,
                plugins: {
                    legend: { display: false },
                    tooltip: { enabled: false }
                },
                scales: {
                    x: { display: false },
                    y: { display: false }
                },
                elements: {
                    point: { radius: 0 }
                }
            }
        });

        return miniChart;
    }

    // 销毁图表
    destroyChart(chartName) {
        if (this.charts[chartName]) {
            this.charts[chartName].destroy();
            delete this.charts[chartName];
        }
    }

    // 销毁所有图表
    destroyAllCharts() {
        Object.keys(this.charts).forEach(chartName => {
            this.destroyChart(chartName);
        });
    }

    // 重置图表大小
    resizeCharts() {
        Object.values(this.charts).forEach(chart => {
            if (chart && typeof chart.resize === 'function') {
                chart.resize();
            }
        });
    }
}

// 创建全局图表管理器
const dashboardCharts = new DashboardCharts();

// 监听窗口大小变化
window.addEventListener('resize', () => {
    dashboardCharts.resizeCharts();
});

// 页面加载完成后初始化图表
document.addEventListener('DOMContentLoaded', () => {
    // 延迟初始化以确保DOM完全加载
    setTimeout(() => {
        dashboardCharts.initCharts();
    }, 100);
});

// 导出供其他脚本使用
window.DashboardCharts = DashboardCharts;
window.dashboardCharts = dashboardCharts;