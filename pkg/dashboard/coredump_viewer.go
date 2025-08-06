package dashboard

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// CoredumpViewer 管理coredump查看器Pod的生命周期
type CoredumpViewer struct {
	kubeClient    kubernetes.Interface
	namespace     string
	config        ViewerConfig
	
	mu            sync.RWMutex
	activeViewers map[string]*ViewerInfo
}

type ViewerConfig struct {
	Enabled           bool   `yaml:"enabled"`
	Image             string `yaml:"image"`
	ImagePullPolicy   string `yaml:"imagePullPolicy"`
	DefaultDuration   int    `yaml:"defaultDuration"`   // 默认持续时间（分钟）
	MaxDuration       int    `yaml:"maxDuration"`       // 最大持续时间（分钟）
	MaxConcurrentPods int    `yaml:"maxConcurrentPods"` // 最大并发Pod数
	CoredumpPath      string `yaml:"coredumpPath"`      // 宿主机coredump路径
	WebTerminalPort   int    `yaml:"webTerminalPort"`   // Web终端端口
}

type ViewerInfo struct {
	ViewerID      string
	CoredumpID    string
	PodName       string
	Namespace     string
	ServiceName   string
	WebTermURL    string
	Status        string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	StopTimer     *time.Timer
}

func NewCoredumpViewer(kubeClient kubernetes.Interface, namespace string, config ViewerConfig) *CoredumpViewer {
	return &CoredumpViewer{
		kubeClient:    kubeClient,
		namespace:     namespace,
		config:        config,
		activeViewers: make(map[string]*ViewerInfo),
	}
}

func (cv *CoredumpViewer) CreateViewer(ctx context.Context, req ViewerRequest) (*ViewerResponse, error) {
	if !cv.config.Enabled {
		return nil, fmt.Errorf("coredump viewer is disabled")
	}

	cv.mu.Lock()
	defer cv.mu.Unlock()

	// 检查并发限制
	if len(cv.activeViewers) >= cv.config.MaxConcurrentPods {
		return nil, fmt.Errorf("maximum concurrent viewers reached (%d)", cv.config.MaxConcurrentPods)
	}

	// 验证持续时间
	duration := req.Duration
	if duration <= 0 {
		duration = cv.config.DefaultDuration
	}
	if duration > cv.config.MaxDuration {
		duration = cv.config.MaxDuration
	}

	// 生成唯一标识符
	viewerID := fmt.Sprintf("viewer-%d", time.Now().Unix())
	podName := fmt.Sprintf("coredump-viewer-%s", viewerID[7:]) // 移除"viewer-"前缀
	serviceName := fmt.Sprintf("svc-%s", podName)

	// 创建Pod
	_, err := cv.createViewerPod(ctx, podName, req.CoredumpID)
	if err != nil {
		return nil, fmt.Errorf("failed to create viewer pod: %w", err)
	}

	// 创建Service
	_, err = cv.createViewerService(ctx, serviceName, podName)
	if err != nil {
		// 清理Pod
		cv.kubeClient.CoreV1().Pods(cv.namespace).Delete(ctx, podName, metav1.DeleteOptions{})
		return nil, fmt.Errorf("failed to create viewer service: %w", err)
	}

	// 构建Web终端URL
	webTermURL := cv.buildWebTermURL(serviceName)

	// 设置过期时间和定时器
	expiresAt := time.Now().Add(time.Duration(duration) * time.Minute)
	
	viewerInfo := &ViewerInfo{
		ViewerID:    viewerID,
		CoredumpID:  req.CoredumpID,
		PodName:     podName,
		Namespace:   cv.namespace,
		ServiceName: serviceName,
		WebTermURL:  webTermURL,
		Status:      "starting",
		CreatedAt:   time.Now(),
		ExpiresAt:   expiresAt,
	}

	// 设置自动清理定时器
	viewerInfo.StopTimer = time.AfterFunc(time.Duration(duration)*time.Minute, func() {
		cv.cleanupViewer(context.Background(), viewerID)
	})

	cv.activeViewers[viewerID] = viewerInfo

	klog.Infof("Created coredump viewer %s for %s, expires at %v", viewerID, req.CoredumpID, expiresAt)

	return &ViewerResponse{
		ViewerID:    viewerID,
		PodName:     podName,
		Namespace:   cv.namespace,
		WebTermURL:  webTermURL,
		ExpiresAt:   expiresAt,
		Status:      "starting",
	}, nil
}

func (cv *CoredumpViewer) GetViewerStatus(viewerID string) (*ViewerResponse, error) {
	cv.mu.RLock()
	defer cv.mu.RUnlock()

	viewerInfo, exists := cv.activeViewers[viewerID]
	if !exists {
		return nil, fmt.Errorf("viewer not found")
	}

	// 获取Pod状态
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	pod, err := cv.kubeClient.CoreV1().Pods(cv.namespace).Get(ctx, viewerInfo.PodName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get pod status for viewer %s: %v", viewerID, err)
		viewerInfo.Status = "error"
	} else {
		viewerInfo.Status = cv.getPodStatus(pod)
	}

	return &ViewerResponse{
		ViewerID:    viewerInfo.ViewerID,
		PodName:     viewerInfo.PodName,
		Namespace:   viewerInfo.Namespace,
		WebTermURL:  viewerInfo.WebTermURL,
		ExpiresAt:   viewerInfo.ExpiresAt,
		Status:      viewerInfo.Status,
	}, nil
}

func (cv *CoredumpViewer) StopViewer(ctx context.Context, viewerID string) error {
	cv.mu.Lock()
	defer cv.mu.Unlock()

	return cv.cleanupViewer(ctx, viewerID)
}

func (cv *CoredumpViewer) cleanupViewer(ctx context.Context, viewerID string) error {
	viewerInfo, exists := cv.activeViewers[viewerID]
	if !exists {
		return fmt.Errorf("viewer not found")
	}

	// 停止定时器
	if viewerInfo.StopTimer != nil {
		viewerInfo.StopTimer.Stop()
	}

	// 删除Service
	if err := cv.kubeClient.CoreV1().Services(cv.namespace).Delete(ctx, viewerInfo.ServiceName, metav1.DeleteOptions{}); err != nil {
		klog.Errorf("Failed to delete service %s: %v", viewerInfo.ServiceName, err)
	}

	// 删除Pod
	if err := cv.kubeClient.CoreV1().Pods(cv.namespace).Delete(ctx, viewerInfo.PodName, metav1.DeleteOptions{}); err != nil {
		klog.Errorf("Failed to delete pod %s: %v", viewerInfo.PodName, err)
	}

	delete(cv.activeViewers, viewerID)
	
	klog.Infof("Cleaned up coredump viewer %s", viewerID)
	return nil
}

func (cv *CoredumpViewer) createViewerPod(ctx context.Context, podName, coredumpID string) (*corev1.Pod, error) {
	// TODO: 在实际实现中，coredumpID应该解析为实际的文件路径
	coredumpPath := fmt.Sprintf("%s/core.milvus_crasher.%s", cv.config.CoredumpPath, coredumpID)
	
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: cv.namespace,
			Labels: map[string]string{
				"app":                        "coredump-viewer",
				"coredump-agent.milvus.io/viewer": "true",
				"coredump-agent.milvus.io/id":     coredumpID,
			},
			Annotations: map[string]string{
				"coredump-agent.milvus.io/created": time.Now().Format(time.RFC3339),
				"coredump-agent.milvus.io/coredump": coredumpPath,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser:  &[]int64{0}[0],
				RunAsGroup: &[]int64{0}[0],
			},
			Containers: []corev1.Container{
				{
					Name:            "debugger",
					Image:           cv.config.Image,
					ImagePullPolicy: corev1.PullPolicy(cv.config.ImagePullPolicy),
					SecurityContext: &corev1.SecurityContext{
						Privileged: &[]bool{true}[0],
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{
								"SYS_ADMIN",
								"SYS_PTRACE",
							},
						},
					},
					Command: []string{"/bin/bash"},
					Args: []string{
						"-c",
						fmt.Sprintf(`
							# 安装web终端工具
							if ! command -v ttyd &> /dev/null; then
								apt-get update && apt-get install -y ttyd gdb || apk add --no-cache ttyd gdb
							fi
							
							# 启动web终端
							ttyd -p %d -W bash -c "echo 'Coredump Viewer Ready'; echo 'Coredump file: %s'; echo 'Use: gdb /path/to/executable %s'; bash"
						`, cv.config.WebTerminalPort, coredumpPath, coredumpPath),
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "web-terminal",
							ContainerPort: int32(cv.config.WebTerminalPort),
							Protocol:      corev1.ProtocolTCP,
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "coredumps",
							MountPath: "/coredumps",
							ReadOnly:  true,
						},
						{
							Name:      "host-root",
							MountPath: "/host",
							ReadOnly:  true,
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "COREDUMP_FILE",
							Value: coredumpPath,
						},
						{
							Name:  "TERM",
							Value: "xterm-256color",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "coredumps",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: cv.config.CoredumpPath,
						},
					},
				},
				{
					Name: "host-root",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/",
						},
					},
				},
			},
			NodeSelector: map[string]string{
				// 确保在有coredump文件的节点上运行
				"kubernetes.io/os": "linux",
			},
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
				},
			},
		},
	}

	return cv.kubeClient.CoreV1().Pods(cv.namespace).Create(ctx, pod, metav1.CreateOptions{})
}

func (cv *CoredumpViewer) createViewerService(ctx context.Context, serviceName, podName string) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: cv.namespace,
			Labels: map[string]string{
				"app": "coredump-viewer",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app": "coredump-viewer",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "web-terminal",
					Port:       int32(cv.config.WebTerminalPort),
					TargetPort: intstr.FromInt(cv.config.WebTerminalPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	return cv.kubeClient.CoreV1().Services(cv.namespace).Create(ctx, service, metav1.CreateOptions{})
}

func (cv *CoredumpViewer) buildWebTermURL(serviceName string) string {
	// 在实际部署中，这里应该根据集群的Ingress配置构建URL
	// 这里提供一个基本的集群内访问URL格式
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceName, cv.namespace, cv.config.WebTerminalPort)
}

func (cv *CoredumpViewer) getPodStatus(pod *corev1.Pod) string {
	switch pod.Status.Phase {
	case corev1.PodRunning:
		// 检查容器是否都就绪
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				return "running"
			}
		}
		return "starting"
	case corev1.PodPending:
		return "pending"
	case corev1.PodSucceeded:
		return "completed"
	case corev1.PodFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// 清理所有过期的查看器
func (cv *CoredumpViewer) CleanupExpiredViewers(ctx context.Context) {
	cv.mu.Lock()
	defer cv.mu.Unlock()

	now := time.Now()
	var expiredViewers []string

	for viewerID, viewerInfo := range cv.activeViewers {
		if now.After(viewerInfo.ExpiresAt) {
			expiredViewers = append(expiredViewers, viewerID)
		}
	}

	for _, viewerID := range expiredViewers {
		cv.cleanupViewer(ctx, viewerID)
	}
}

// 获取活跃查看器列表
func (cv *CoredumpViewer) GetActiveViewers() map[string]*ViewerInfo {
	cv.mu.RLock()
	defer cv.mu.RUnlock()

	// 返回副本以避免并发访问问题
	result := make(map[string]*ViewerInfo)
	for k, v := range cv.activeViewers {
		result[k] = v
	}
	return result
}

// 启动后台清理任务
func (cv *CoredumpViewer) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cv.CleanupExpiredViewers(ctx)
		}
	}
}