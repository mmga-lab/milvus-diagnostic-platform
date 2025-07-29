package collector

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"milvus-coredump-agent/pkg/config"
	"milvus-coredump-agent/pkg/discovery"
)

type Collector struct {
	config         *config.CollectorConfig
	discovery      *discovery.Discovery
	eventChan      chan CollectionEvent
	stopChan       chan struct{}
	processedFiles map[string]bool
}

var (
	coredumpPattern = regexp.MustCompile(`^core\.([^.]+)\.(\d+)\.(\d+)\.(\d+)$`)
	systemdPattern  = regexp.MustCompile(`^core\.([^.]+)\.(\d+)\.([0-9a-f]+)\.(\d+)\.(\d+)$`)
)

func New(config *config.CollectorConfig, discovery *discovery.Discovery) *Collector {
	return &Collector{
		config:         config,
		discovery:      discovery,
		eventChan:      make(chan CollectionEvent, 100),
		stopChan:       make(chan struct{}),
		processedFiles: make(map[string]bool),
	}
}

func (c *Collector) Start(ctx context.Context) error {
	klog.Info("Starting coredump collector")

	go c.watchRestartEvents(ctx)
	go c.scanCoredumpFiles(ctx)

	<-ctx.Done()
	close(c.stopChan)
	return nil
}

func (c *Collector) GetEventChannel() <-chan CollectionEvent {
	return c.eventChan
}

func (c *Collector) watchRestartEvents(ctx context.Context) {
	restartChan := c.discovery.GetRestartChannel()
	
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-restartChan:
			c.handleRestartEvent(event)
		}
	}
}

func (c *Collector) handleRestartEvent(event discovery.RestartEvent) {
	klog.Infof("Handling restart event for pod %s/%s", event.PodNamespace, event.PodName)
	
	collectionEvent := CollectionEvent{
		Type:         EventTypeRestartDetected,
		RestartEvent: &event,
		Timestamp:    time.Now(),
	}
	
	select {
	case c.eventChan <- collectionEvent:
	default:
		klog.Warning("Event channel is full, dropping restart event")
	}

	if event.IsPanic {
		go c.collectCoredumpForRestart(event)
	}
}

func (c *Collector) collectCoredumpForRestart(event discovery.RestartEvent) {
	maxWait := 30 * time.Second
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	
	timeout := time.After(maxWait)
	
	for {
		select {
		case <-timeout:
			klog.Warningf("Timeout waiting for coredump file for restart event %s/%s", 
				event.PodNamespace, event.PodName)
			return
		case <-ticker.C:
			if files := c.findCoredumpForRestart(event); len(files) > 0 {
				for _, file := range files {
					c.processCoredumpFile(file)
				}
				return
			}
		}
	}
}

func (c *Collector) findCoredumpForRestart(event discovery.RestartEvent) []*CoredumpFile {
	var files []*CoredumpFile
	
	err := filepath.Walk(c.config.CoredumpPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		
		if info.IsDir() {
			return nil
		}
		
		if !c.isCoredumpFile(info.Name()) {
			return nil
		}

		if time.Since(info.ModTime()) > 2*time.Minute {
			return nil
		}
		
		if c.processedFiles[path] {
			return nil
		}
		
		coredumpFile := c.parseCoredumpFile(path, info)
		if coredumpFile != nil && c.isRelatedToRestart(coredumpFile, event) {
			files = append(files, coredumpFile)
		}
		
		return nil
	})
	
	if err != nil {
		klog.Errorf("Error walking coredump directory: %v", err)
	}
	
	return files
}

func (c *Collector) scanCoredumpFiles(ctx context.Context) {
	ticker := time.NewTicker(c.config.WatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.scanDirectory()
		}
	}
}

func (c *Collector) scanDirectory() {
	err := filepath.Walk(c.config.CoredumpPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		
		if info.IsDir() {
			return nil
		}
		
		if !c.isCoredumpFile(info.Name()) {
			return nil
		}
		
		if time.Since(info.ModTime()) > c.config.MaxFileAge {
			return nil
		}
		
		if c.processedFiles[path] {
			return nil
		}
		
		coredumpFile := c.parseCoredumpFile(path, info)
		if coredumpFile != nil {
			c.processCoredumpFile(coredumpFile)
		}
		
		return nil
	})
	
	if err != nil {
		klog.Errorf("Error scanning coredump directory: %v", err)
	}
}

func (c *Collector) isCoredumpFile(filename string) bool {
	return coredumpPattern.MatchString(filename) || 
		   systemdPattern.MatchString(filename) ||
		   strings.HasPrefix(filename, "core.")
}

func (c *Collector) parseCoredumpFile(path string, info os.FileInfo) *CoredumpFile {
	filename := info.Name()
	
	coredump := &CoredumpFile{
		Path:      path,
		FileName:  filename,
		Size:      info.Size(),
		ModTime:   info.ModTime(),
		Timestamp: info.ModTime(),
		Status:    StatusDiscovered,
		CreatedAt: metav1.Now(),
		UpdatedAt: metav1.Now(),
	}

	if matches := coredumpPattern.FindStringSubmatch(filename); len(matches) >= 5 {
		coredump.Executable = matches[1]
		if pid, err := strconv.Atoi(matches[2]); err == nil {
			coredump.PID = pid
		}
		if uid, err := strconv.Atoi(matches[3]); err == nil {
			coredump.UID = uid
		}
		if signal, err := strconv.Atoi(matches[4]); err == nil {
			coredump.Signal = signal
		}
	} else if matches := systemdPattern.FindStringSubmatch(filename); len(matches) >= 6 {
		coredump.Executable = matches[1]
		if pid, err := strconv.Atoi(matches[2]); err == nil {
			coredump.PID = pid
		}
		if uid, err := strconv.ParseInt(matches[4], 16, 32); err == nil {
			coredump.UID = int(uid)
		}
		if signal, err := strconv.Atoi(matches[5]); err == nil {
			coredump.Signal = signal
		}
	}

	c.enrichWithPodInfo(coredump)
	
	return coredump
}

func (c *Collector) enrichWithPodInfo(coredump *CoredumpFile) {
	instances := c.discovery.GetInstances()
	
	for _, instance := range instances {
		for _, pod := range instance.Pods {
			if c.isPodRelatedToCoredump(pod, coredump) {
				coredump.PodName = pod.Name
				coredump.PodNamespace = pod.Namespace
				coredump.InstanceName = instance.Name
				
				for _, containerStatus := range pod.ContainerStatuses {
					if strings.Contains(coredump.Executable, containerStatus.Name) {
						coredump.ContainerName = containerStatus.Name
						break
					}
				}
				return
			}
		}
	}
}

func (c *Collector) isPodRelatedToCoredump(pod discovery.PodInfo, coredump *CoredumpFile) bool {
	if strings.Contains(coredump.Executable, "milvus") {
		return true
	}
	
	timeDiff := coredump.ModTime.Sub(pod.LastRestart.Time).Abs()
	if timeDiff < 5*time.Minute {
		return true
	}
	
	return false
}

func (c *Collector) isRelatedToRestart(coredump *CoredumpFile, event discovery.RestartEvent) bool {
	if coredump.PodName == event.PodName && coredump.PodNamespace == event.PodNamespace {
		return true
	}
	
	timeDiff := coredump.ModTime.Sub(event.RestartTime.Time).Abs()
	if timeDiff < 2*time.Minute {
		return true
	}
	
	return false
}

func (c *Collector) processCoredumpFile(coredump *CoredumpFile) {
	c.processedFiles[coredump.Path] = true
	
	klog.Infof("Processing coredump file: %s", coredump.Path)
	
	coredump.Status = StatusProcessing
	coredump.UpdatedAt = metav1.Now()
	
	event := CollectionEvent{
		Type:         EventTypeFileDiscovered,
		CoredumpFile: coredump,
		Timestamp:    time.Now(),
	}
	
	select {
	case c.eventChan <- event:
	default:
		klog.Warning("Event channel is full, dropping file event")
	}
}

func (c *Collector) GetProcessedFiles() map[string]bool {
	return c.processedFiles
}