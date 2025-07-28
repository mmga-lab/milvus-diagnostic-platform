package cleaner

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"milvus-coredump-agent/pkg/config"
	"milvus-coredump-agent/pkg/discovery"
	"milvus-coredump-agent/pkg/storage"
)

type Cleaner struct {
	config        *config.CleanerConfig
	kubeClient    kubernetes.Interface
	discovery     *discovery.Discovery
	restartCounts map[string]*RestartTracker
	mu            sync.RWMutex
	eventChan     chan CleanupEvent
}

type RestartTracker struct {
	Count       int
	FirstRestart time.Time
	LastRestart  time.Time
	InstanceName string
	Namespace    string
	Cleaned      bool
}

type CleanupEvent struct {
	Type         EventType `json:"type"`
	InstanceName string    `json:"instanceName"`
	Namespace    string    `json:"namespace"`
	Reason       string    `json:"reason"`
	Error        string    `json:"error,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

type EventType string

const (
	EventTypeInstanceUninstalled EventType = "instance_uninstalled"
	EventTypeCleanupSkipped      EventType = "cleanup_skipped"
	EventTypeCleanupError        EventType = "cleanup_error"
	EventTypeRestartThreshold    EventType = "restart_threshold_exceeded"
)

func New(config *config.CleanerConfig, kubeClient kubernetes.Interface, discovery *discovery.Discovery) *Cleaner {
	return &Cleaner{
		config:        config,
		kubeClient:    kubeClient,
		discovery:     discovery,
		restartCounts: make(map[string]*RestartTracker),
		eventChan:     make(chan CleanupEvent, 100),
	}
}

func (c *Cleaner) Start(ctx context.Context, storageEvents <-chan storage.StorageEvent) error {
	if !c.config.Enabled {
		klog.Info("Auto cleanup is disabled")
		return nil
	}

	klog.Info("Starting auto cleanup manager")

	go c.monitorRestartEvents(ctx)
	go c.monitorStorageEvents(ctx, storageEvents)
	go c.periodicCleanup(ctx)

	<-ctx.Done()
	return nil
}

func (c *Cleaner) GetEventChannel() <-chan CleanupEvent {
	return c.eventChan
}

func (c *Cleaner) monitorRestartEvents(ctx context.Context) {
	restartChan := c.discovery.GetRestartChannel()

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-restartChan:
			if event.IsPanic {
				c.handleRestartEvent(event)
			}
		}
	}
}

func (c *Cleaner) monitorStorageEvents(ctx context.Context, storageEvents <-chan storage.StorageEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-storageEvents:
			if event.Type == storage.EventTypeFileStored && event.CoredumpFile != nil {
				c.evaluateForCleanup(event.CoredumpFile.InstanceName, event.CoredumpFile.PodNamespace)
			}
		}
	}
}

func (c *Cleaner) handleRestartEvent(event discovery.RestartEvent) {
	key := fmt.Sprintf("%s/%s", event.PodNamespace, event.InstanceName)

	c.mu.Lock()
	defer c.mu.Unlock()

	tracker, exists := c.restartCounts[key]
	if !exists {
		tracker = &RestartTracker{
			Count:        1,
			FirstRestart: event.RestartTime.Time,
			LastRestart:  event.RestartTime.Time,
			InstanceName: event.InstanceName,
			Namespace:    event.PodNamespace,
			Cleaned:      false,
		}
		c.restartCounts[key] = tracker
	} else {
		if time.Since(tracker.FirstRestart) > c.config.RestartTimeWindow {
			tracker.Count = 1
			tracker.FirstRestart = event.RestartTime.Time
		} else {
			tracker.Count++
		}
		tracker.LastRestart = event.RestartTime.Time
	}

	klog.V(2).Infof("Restart count for %s: %d (within %v window)", 
		key, tracker.Count, c.config.RestartTimeWindow)

	if tracker.Count >= c.config.MaxRestartCount && !tracker.Cleaned {
		klog.Warningf("Instance %s has exceeded restart threshold (%d), scheduling for cleanup", 
			key, c.config.MaxRestartCount)

		cleanupEvent := CleanupEvent{
			Type:         EventTypeRestartThreshold,
			InstanceName: event.InstanceName,
			Namespace:    event.PodNamespace,
			Reason:       fmt.Sprintf("Exceeded restart threshold: %d restarts in %v", tracker.Count, c.config.RestartTimeWindow),
			Timestamp:    time.Now(),
		}
		c.sendEvent(cleanupEvent)

		go c.scheduleCleanup(event.InstanceName, event.PodNamespace, tracker)
	}
}

func (c *Cleaner) scheduleCleanup(instanceName, namespace string, tracker *RestartTracker) {
	time.Sleep(c.config.CleanupDelay)

	key := fmt.Sprintf("%s/%s", namespace, instanceName)
	
	c.mu.Lock()
	if tracker.Cleaned {
		c.mu.Unlock()
		klog.V(2).Infof("Instance %s already cleaned, skipping", key)
		return
	}
	tracker.Cleaned = true
	c.mu.Unlock()

	if err := c.cleanupInstance(instanceName, namespace); err != nil {
		klog.Errorf("Failed to cleanup instance %s: %v", key, err)
		
		event := CleanupEvent{
			Type:         EventTypeCleanupError,
			InstanceName: instanceName,
			Namespace:    namespace,
			Error:        err.Error(),
			Timestamp:    time.Now(),
		}
		c.sendEvent(event)
		
		c.mu.Lock()
		tracker.Cleaned = false
		c.mu.Unlock()
	} else {
		klog.Infof("Successfully cleaned up instance: %s", key)
		
		event := CleanupEvent{
			Type:         EventTypeInstanceUninstalled,
			InstanceName: instanceName,
			Namespace:    namespace,
			Reason:       "Automatic cleanup due to repeated crashes",
			Timestamp:    time.Now(),
		}
		c.sendEvent(event)
	}
}

func (c *Cleaner) evaluateForCleanup(instanceName, namespace string) {
	if instanceName == "" || namespace == "" {
		return
	}

	key := fmt.Sprintf("%s/%s", namespace, instanceName)
	
	c.mu.RLock()
	tracker, exists := c.restartCounts[key]
	c.mu.RUnlock()

	if exists && tracker.Count >= c.config.MaxRestartCount && !tracker.Cleaned {
		klog.Infof("Evaluating instance %s for immediate cleanup due to stored coredump", key)
		go c.scheduleCleanup(instanceName, namespace, tracker)
	}
}

func (c *Cleaner) cleanupInstance(instanceName, namespace string) error {
	instances := c.discovery.GetInstances()
	instanceKey := fmt.Sprintf("%s/%s", namespace, instanceName)
	
	instance, exists := instances[instanceKey]
	if !exists {
		return fmt.Errorf("instance not found: %s", instanceKey)
	}

	switch instance.Type {
	case discovery.DeploymentTypeHelm:
		return c.uninstallHelmRelease(instanceName, namespace)
	case discovery.DeploymentTypeOperator:
		return c.deleteOperatorInstance(instanceName, namespace)
	default:
		return fmt.Errorf("unsupported deployment type: %s", instance.Type)
	}
}

func (c *Cleaner) uninstallHelmRelease(releaseName, namespace string) error {
	klog.Infof("Uninstalling Helm release: %s in namespace %s", releaseName, namespace)

	ctx, cancel := context.WithTimeout(context.Background(), c.config.UninstallTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "helm", "uninstall", releaseName, "-n", namespace)
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		if strings.Contains(string(output), "not found") {
			klog.Infof("Helm release %s not found, may already be uninstalled", releaseName)
			return nil
		}
		return fmt.Errorf("helm uninstall failed: %v, output: %s", err, string(output))
	}

	klog.Infof("Helm release %s uninstalled successfully", releaseName)
	return nil
}

func (c *Cleaner) deleteOperatorInstance(instanceName, namespace string) error {
	klog.Infof("Deleting Milvus operator instance: %s in namespace %s", instanceName, namespace)

	ctx, cancel := context.WithTimeout(context.Background(), c.config.UninstallTimeout)
	defer cancel()

	deleteOptions := metav1.DeleteOptions{}
	err := c.kubeClient.CoreV1().
		Pods(namespace).
		DeleteCollection(ctx, deleteOptions, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", instanceName),
		})

	if err != nil {
		return fmt.Errorf("failed to delete operator instance pods: %w", err)
	}

	err = c.kubeClient.AppsV1().
		Deployments(namespace).
		DeleteCollection(ctx, deleteOptions, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", instanceName),
		})

	if err != nil {
		return fmt.Errorf("failed to delete operator instance deployments: %w", err)
	}

	klog.Infof("Milvus operator instance %s deleted successfully", instanceName)
	return nil
}

func (c *Cleaner) periodicCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.cleanupOldTrackers()
		}
	}
}

func (c *Cleaner) cleanupOldTrackers() {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	
	for key, tracker := range c.restartCounts {
		if tracker.LastRestart.Before(cutoff) {
			delete(c.restartCounts, key)
			klog.V(2).Infof("Removed old restart tracker for %s", key)
		}
	}
}

func (c *Cleaner) GetRestartCounts() map[string]*RestartTracker {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*RestartTracker)
	for k, v := range c.restartCounts {
		result[k] = &RestartTracker{
			Count:        v.Count,
			FirstRestart: v.FirstRestart,
			LastRestart:  v.LastRestart,
			InstanceName: v.InstanceName,
			Namespace:    v.Namespace,
			Cleaned:      v.Cleaned,
		}
	}
	
	return result
}

func (c *Cleaner) sendEvent(event CleanupEvent) {
	select {
	case c.eventChan <- event:
	default:
		klog.Warning("Cleanup event channel is full, dropping event")
	}
}