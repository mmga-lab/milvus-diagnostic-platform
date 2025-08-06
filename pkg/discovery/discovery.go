package discovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"milvus-diagnostic-platform/pkg/config"
)

type Discovery struct {
	client      kubernetes.Interface
	config      *config.DiscoveryConfig
	instances   map[string]*MilvusInstance
	restartChan chan RestartEvent
	stopChan    chan struct{}
}

func New(client kubernetes.Interface, config *config.DiscoveryConfig) *Discovery {
	return &Discovery{
		client:      client,
		config:      config,
		instances:   make(map[string]*MilvusInstance),
		restartChan: make(chan RestartEvent, 100),
		stopChan:    make(chan struct{}),
	}
}

func (d *Discovery) Start(ctx context.Context) error {
	klog.Info("Starting Milvus instance discovery")

	go d.scanInstances(ctx)
	go d.watchPodEvents(ctx)

	<-ctx.Done()
	close(d.stopChan)
	return nil
}

func (d *Discovery) GetRestartChannel() <-chan RestartEvent {
	return d.restartChan
}

func (d *Discovery) GetDiscoveredInstances() []*MilvusInstance {
	var instances []*MilvusInstance
	for _, instance := range d.instances {
		instances = append(instances, instance)
	}
	return instances
}

func (d *Discovery) GetInstances() map[string]*MilvusInstance {
	return d.instances
}

func (d *Discovery) scanInstances(ctx context.Context) {
	ticker := time.NewTicker(d.config.ScanInterval)
	defer ticker.Stop()

	// Scan immediately on startup
	klog.Info("Starting initial Milvus instance scan...")
	d.discoverInstances(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.discoverInstances(ctx)
		}
	}
}

func (d *Discovery) discoverInstances(ctx context.Context) {
	klog.Infof("Scanning for Milvus instances in namespaces: %v", d.config.Namespaces)
	for _, namespace := range d.config.Namespaces {
		if err := d.discoverInNamespace(ctx, namespace); err != nil {
			klog.Errorf("Failed to discover instances in namespace %s: %v", namespace, err)
		}
	}
}

func (d *Discovery) discoverInNamespace(ctx context.Context, namespace string) error {
	pods, err := d.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	klog.Infof("Found %d pods in namespace %s", len(pods.Items), namespace)
	instanceMap := make(map[string]*MilvusInstance)

	for _, pod := range pods.Items {
		if instance := d.identifyMilvusInstance(&pod); instance != nil {
			key := fmt.Sprintf("%s/%s", instance.Namespace, instance.Name)
			if existing, exists := instanceMap[key]; exists {
				existing.Pods = append(existing.Pods, d.createPodInfo(&pod))
			} else {
				instance.Pods = append(instance.Pods, d.createPodInfo(&pod))
				instanceMap[key] = instance
			}
		}
	}

	for key, instance := range instanceMap {
		d.instances[key] = instance
		klog.V(2).Infof("Discovered Milvus instance: %s", key)
	}

	return nil
}

func (d *Discovery) identifyMilvusInstance(pod *corev1.Pod) *MilvusInstance {
	deploymentType := d.getDeploymentType(pod)
	if deploymentType == "" {
		klog.V(4).Infof("Pod %s/%s is not a Milvus instance", pod.Namespace, pod.Name)
		return nil
	}
	klog.Infof("Identified Milvus pod %s/%s as %s deployment", pod.Namespace, pod.Name, deploymentType)

	instanceName := d.extractInstanceName(pod, deploymentType)
	if instanceName == "" {
		return nil
	}

	return &MilvusInstance{
		Name:        instanceName,
		Namespace:   pod.Namespace,
		Type:        DeploymentType(deploymentType),
		Labels:      pod.Labels,
		Annotations: pod.Annotations,
		Status:      d.getInstanceStatus(pod),
		CreatedAt:   pod.CreationTimestamp,
		Pods:        []PodInfo{},
	}
}

func (d *Discovery) getDeploymentType(pod *corev1.Pod) string {
	labels := pod.Labels
	
	for _, helmLabel := range d.config.HelmReleaseLabels {
		parts := strings.Split(helmLabel, "=")
		if len(parts) == 2 {
			key, value := parts[0], parts[1]
			if labels[key] == value {
				return "helm"
			}
		} else {
			if _, exists := labels[helmLabel]; exists {
				return "helm"
			}
		}
	}

	for _, operatorLabel := range d.config.OperatorLabels {
		parts := strings.Split(operatorLabel, "=")
		if len(parts) == 2 {
			key, value := parts[0], parts[1]
			if labels[key] == value {
				return "operator"
			}
		} else {
			if _, exists := labels[operatorLabel]; exists {
				return "operator"
			}
		}
	}

	return ""
}

func (d *Discovery) extractInstanceName(pod *corev1.Pod, deploymentType string) string {
	labels := pod.Labels
	
	if deploymentType == "helm" {
		if releaseName, exists := labels["app.kubernetes.io/instance"]; exists {
			return releaseName
		}
		if releaseName, exists := labels["helm.sh/release"]; exists {
			return releaseName
		}
	}
	
	if deploymentType == "operator" {
		if instanceName, exists := labels["app.kubernetes.io/name"]; exists {
			return instanceName
		}
		if instanceName, exists := labels["milvus.io/instance"]; exists {
			return instanceName
		}
	}

	return pod.Name
}

func (d *Discovery) getInstanceStatus(pod *corev1.Pod) InstanceStatus {
	switch pod.Status.Phase {
	case corev1.PodRunning:
		return InstanceStatusRunning
	case corev1.PodFailed:
		return InstanceStatusFailed
	case corev1.PodPending:
		return InstanceStatusPending
	default:
		return InstanceStatusTerminating
	}
}

func (d *Discovery) createPodInfo(pod *corev1.Pod) PodInfo {
	var restartCount int32
	var lastRestart metav1.Time
	var containerStatuses []ContainerStatusInfo

	for _, containerStatus := range pod.Status.ContainerStatuses {
		restartCount += containerStatus.RestartCount
		
		if containerStatus.LastTerminationState.Terminated != nil {
			if containerStatus.LastTerminationState.Terminated.FinishedAt.After(lastRestart.Time) {
				lastRestart = containerStatus.LastTerminationState.Terminated.FinishedAt
			}
		}

		containerStatuses = append(containerStatuses, ContainerStatusInfo{
			Name:         containerStatus.Name,
			RestartCount: containerStatus.RestartCount,
			Ready:        containerStatus.Ready,
			LastTerminationReason: func() string {
				if containerStatus.LastTerminationState.Terminated != nil {
					return containerStatus.LastTerminationState.Terminated.Reason
				}
				return ""
			}(),
			LastTerminationMessage: func() string {
				if containerStatus.LastTerminationState.Terminated != nil {
					return containerStatus.LastTerminationState.Terminated.Message
				}
				return ""
			}(),
		})
	}

	return PodInfo{
		Name:              pod.Name,
		Namespace:         pod.Namespace,
		Status:            string(pod.Status.Phase),
		RestartCount:      restartCount,
		LastRestart:       lastRestart,
		ContainerStatuses: containerStatuses,
	}
}

func (d *Discovery) watchPodEvents(ctx context.Context) {
	for _, namespace := range d.config.Namespaces {
		go d.watchPodsInNamespace(ctx, namespace)
	}
}

func (d *Discovery) watchPodsInNamespace(ctx context.Context, namespace string) {
	watchlist := cache.NewListWatchFromClient(
		d.client.CoreV1().RESTClient(),
		"pods",
		namespace,
		fields.Everything(),
	)

	_, controller := cache.NewInformer(
		watchlist,
		&corev1.Pod{},
		time.Second*10,
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldPod := oldObj.(*corev1.Pod)
				newPod := newObj.(*corev1.Pod)
				d.checkForRestarts(oldPod, newPod)
			},
		},
	)

	go controller.Run(d.stopChan)
}

func (d *Discovery) checkForRestarts(oldPod, newPod *corev1.Pod) {
	if d.identifyMilvusInstance(newPod) == nil {
		return
	}
	klog.V(3).Infof("Checking for restarts: pod %s/%s", newPod.Namespace, newPod.Name)

	for i, newStatus := range newPod.Status.ContainerStatuses {
		if i >= len(oldPod.Status.ContainerStatuses) {
			continue
		}
		
		oldStatus := oldPod.Status.ContainerStatuses[i]
		if newStatus.RestartCount > oldStatus.RestartCount {
			klog.Infof("Detected restart for pod %s/%s: %s (old: %d, new: %d)", 
				newPod.Namespace, newPod.Name, newStatus.Name, 
				oldStatus.RestartCount, newStatus.RestartCount)
			event := d.createRestartEvent(newPod, newStatus)
			select {
			case d.restartChan <- event:
				klog.Infof("Sent restart event for pod %s/%s", newPod.Namespace, newPod.Name)
			default:
				klog.Warning("Restart event channel is full, dropping event")
			}
		}
	}
}

func (d *Discovery) createRestartEvent(pod *corev1.Pod, containerStatus corev1.ContainerStatus) RestartEvent {
	var reason, message string
	var exitCode, signal int32
	
	if containerStatus.LastTerminationState.Terminated != nil {
		term := containerStatus.LastTerminationState.Terminated
		reason = term.Reason
		message = term.Message
		exitCode = term.ExitCode
		signal = term.Signal
	}

	instance := d.identifyMilvusInstance(pod)
	instanceName := ""
	if instance != nil {
		instanceName = instance.Name
	}

	isPanic := d.isPanicRestart(reason, message, exitCode, signal)

	return RestartEvent{
		PodName:       pod.Name,
		PodNamespace:  pod.Namespace,
		ContainerName: containerStatus.Name,
		RestartTime:   metav1.Now(),
		Reason:        reason,
		Message:       message,
		ExitCode:      exitCode,
		Signal:        signal,
		InstanceName:  instanceName,
		IsPanic:       isPanic,
	}
}

func (d *Discovery) isPanicRestart(reason, message string, exitCode, signal int32) bool {
	reasonLower := strings.ToLower(reason)
	messageLower := strings.ToLower(message)
	
	if strings.Contains(reasonLower, "liveness") || 
	   strings.Contains(reasonLower, "readiness") || 
	   strings.Contains(reasonLower, "startup") {
		return false
	}

	panicIndicators := []string{"panic", "fatal", "sigsegv", "sigabrt", "sigfpe", "assertion failed"}
	
	for _, indicator := range panicIndicators {
		if strings.Contains(reasonLower, indicator) || strings.Contains(messageLower, indicator) {
			return true
		}
	}

	if signal == 11 || signal == 6 || signal == 8 {
		return true
	}

	if exitCode != 0 && exitCode != 1 && exitCode != 130 && exitCode != 143 {
		return true
	}

	return false
}