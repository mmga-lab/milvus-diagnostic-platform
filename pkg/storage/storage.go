package storage

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"k8s.io/klog/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"milvus-coredump-agent/pkg/analyzer"
	"milvus-coredump-agent/pkg/collector"
	"milvus-coredump-agent/pkg/config"
)

type Storage struct {
	config         *config.StorageConfig
	analyzerConfig *config.AnalyzerConfig
	backend        Backend
	eventChan      chan StorageEvent
}

type Backend interface {
	Store(ctx context.Context, file *collector.CoredumpFile, reader io.Reader) error
	Delete(ctx context.Context, path string) error
	List(ctx context.Context) ([]*StoredFile, error)
	GetStorageSize(ctx context.Context) (int64, error)
}

type StorageEvent struct {
	Type         EventType               `json:"type"`
	CoredumpFile *collector.CoredumpFile `json:"coredumpFile,omitempty"`
	Error        string                  `json:"error,omitempty"`
	Timestamp    time.Time               `json:"timestamp"`
}

type EventType string

const (
	EventTypeFileStored   EventType = "file_stored"
	EventTypeFileDeleted  EventType = "file_deleted"
	EventTypeStorageError EventType = "storage_error"
	EventTypeCleanupDone  EventType = "cleanup_done"
)

type StoredFile struct {
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	StoredAt     time.Time `json:"storedAt"`
	ValueScore   float64   `json:"valueScore"`
	InstanceName string    `json:"instanceName"`
}

func New(config *config.StorageConfig, analyzerConfig *config.AnalyzerConfig) (*Storage, error) {
	var backend Backend
	var err error

	switch config.Backend {
	case "local":
		backend, err = NewLocalBackend(config)
	case "s3":
		backend, err = NewS3Backend(config)
	case "nfs":
		backend, err = NewNFSBackend(config)
	default:
		return nil, fmt.Errorf("unsupported storage backend: %s", config.Backend)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create storage backend: %w", err)
	}

	return &Storage{
		config:         config,
		analyzerConfig: analyzerConfig,
		backend:   backend,
		eventChan: make(chan StorageEvent, 100),
	}, nil
}

func (s *Storage) Start(ctx context.Context, analyzerChan <-chan analyzer.AnalysisEvent) error {
	klog.Info("Starting storage manager")

	go s.processAnalysisEvents(ctx, analyzerChan)
	go s.periodicCleanup(ctx)

	<-ctx.Done()
	return nil
}

func (s *Storage) GetEventChannel() <-chan StorageEvent {
	return s.eventChan
}

func (s *Storage) processAnalysisEvents(ctx context.Context, analyzerChan <-chan analyzer.AnalysisEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-analyzerChan:
			switch event.Type {
			case analyzer.EventTypeAnalysisComplete:
				if event.CoredumpFile != nil {
					go s.handleAnalyzedFile(ctx, event.CoredumpFile)
				}
			case analyzer.EventTypeAnalysisSkipped:
				if event.CoredumpFile != nil {
					klog.V(2).Infof("Skipping storage for analyzed file: %s", event.CoredumpFile.Path)
				}
			}
		}
	}
}

func (s *Storage) handleAnalyzedFile(ctx context.Context, coredump *collector.CoredumpFile) {
	if coredump.ValueScore < s.analyzerConfig.ValueThreshold {
		klog.Infof("Skipping storage for low-value coredump: %s (score: %.2f)", 
			coredump.Path, coredump.ValueScore)
		return
	}

	klog.Infof("Storing coredump file: %s (score: %.2f)", coredump.Path, coredump.ValueScore)

	if err := s.storeFile(ctx, coredump); err != nil {
		klog.Errorf("Failed to store coredump %s: %v", coredump.Path, err)
		
		event := StorageEvent{
			Type:         EventTypeStorageError,
			CoredumpFile: coredump,
			Error:        err.Error(),
			Timestamp:    time.Now(),
		}
		s.sendEvent(event)
		return
	}

	coredump.Status = collector.StatusStored
	coredump.UpdatedAt = metav1.Now()

	event := StorageEvent{
		Type:         EventTypeFileStored,
		CoredumpFile: coredump,
		Timestamp:    time.Now(),
	}
	s.sendEvent(event)
}

func (s *Storage) storeFile(ctx context.Context, coredump *collector.CoredumpFile) error {
	file, err := os.Open(coredump.Path)
	if err != nil {
		return fmt.Errorf("failed to open coredump file: %w", err)
	}
	defer file.Close()

	var reader io.Reader = file

	if s.config.CompressionEnabled {
		reader, err = s.compressReader(file)
		if err != nil {
			return fmt.Errorf("failed to compress file: %w", err)
		}
	}

	return s.backend.Store(ctx, coredump, reader)
}

func (s *Storage) compressReader(reader io.Reader) (io.Reader, error) {
	pr, pw := io.Pipe()
	
	go func() {
		defer pw.Close()
		
		gzWriter := gzip.NewWriter(pw)
		defer gzWriter.Close()
		
		if _, err := io.Copy(gzWriter, reader); err != nil {
			pw.CloseWithError(err)
		}
	}()
	
	return pr, nil
}

func (s *Storage) periodicCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.performCleanup(ctx); err != nil {
				klog.Errorf("Failed to perform storage cleanup: %v", err)
			}
		}
	}
}

func (s *Storage) performCleanup(ctx context.Context) error {
	klog.Info("Starting storage cleanup")

	files, err := s.backend.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list stored files: %w", err)
	}

	now := time.Now()
	retentionTime := time.Duration(s.config.RetentionDays) * 24 * time.Hour

	var filesToDelete []*StoredFile
	var totalSize int64

	for _, file := range files {
		totalSize += file.Size
		
		if now.Sub(file.StoredAt) > retentionTime {
			filesToDelete = append(filesToDelete, file)
		}
	}

	maxSize := s.parseSize(s.config.MaxStorageSize)
	if totalSize > maxSize {
		klog.Infof("Storage size (%d) exceeds limit (%d), cleaning up low-value files", 
			totalSize, maxSize)
		
		sort.Slice(files, func(i, j int) bool {
			return files[i].ValueScore < files[j].ValueScore
		})
		
		for _, file := range files {
			if totalSize <= maxSize {
				break
			}
			filesToDelete = append(filesToDelete, file)
			totalSize -= file.Size
		}
	}

	deletedCount := 0
	for _, file := range filesToDelete {
		if err := s.backend.Delete(ctx, file.Path); err != nil {
			klog.Errorf("Failed to delete file %s: %v", file.Path, err)
			continue
		}
		deletedCount++
		klog.V(2).Infof("Deleted old coredump file: %s", file.Path)
	}

	klog.Infof("Storage cleanup completed, deleted %d files", deletedCount)

	event := StorageEvent{
		Type:      EventTypeCleanupDone,
		Timestamp: time.Now(),
	}
	s.sendEvent(event)

	return nil
}

func (s *Storage) parseSize(sizeStr string) int64 {
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))
	
	var multiplier int64 = 1
	if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "GB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		sizeStr = strings.TrimSuffix(sizeStr, "KB")
	}

	var size int64 = 50 * 1024 * 1024 * 1024 // default 50GB
	if _, err := fmt.Sscanf(sizeStr, "%d", &size); err == nil {
		size *= multiplier
	}

	return size
}

func (s *Storage) sendEvent(event StorageEvent) {
	select {
	case s.eventChan <- event:
	default:
		klog.Warning("Storage event channel is full, dropping event")
	}
}

type LocalBackend struct {
	basePath string
}

func NewLocalBackend(config *config.StorageConfig) (*LocalBackend, error) {
	if err := os.MkdirAll(config.LocalPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create local storage directory: %w", err)
	}

	return &LocalBackend{
		basePath: config.LocalPath,
	}, nil
}

func (b *LocalBackend) Store(ctx context.Context, file *collector.CoredumpFile, reader io.Reader) error {
	filename := b.generateStorageFilename(file)
	fullPath := filepath.Join(b.basePath, filename)

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	outFile, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, reader); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

func (b *LocalBackend) Delete(ctx context.Context, path string) error {
	fullPath := filepath.Join(b.basePath, path)
	return os.Remove(fullPath)
}

func (b *LocalBackend) List(ctx context.Context) ([]*StoredFile, error) {
	var files []*StoredFile

	err := filepath.Walk(b.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(b.basePath, path)
		
		file := &StoredFile{
			Path:     relPath,
			Size:     info.Size(),
			StoredAt: info.ModTime(),
		}

		files = append(files, file)
		return nil
	})

	return files, err
}

func (b *LocalBackend) GetStorageSize(ctx context.Context) (int64, error) {
	var totalSize int64

	err := filepath.Walk(b.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})

	return totalSize, err
}

func (b *LocalBackend) generateStorageFilename(file *collector.CoredumpFile) string {
	timestamp := file.Timestamp.Format("2006-01-02_15-04-05")
	
	if file.InstanceName != "" && file.PodName != "" {
		return filepath.Join(
			file.InstanceName,
			fmt.Sprintf("%s_%s_%s.core.gz", timestamp, file.PodName, file.ContainerName),
		)
	}
	
	return fmt.Sprintf("%s_%s.core.gz", timestamp, file.FileName)
}

type S3Backend struct {
	config *config.S3Config
}

func NewS3Backend(config *config.StorageConfig) (*S3Backend, error) {
	return &S3Backend{
		config: &config.S3,
	}, nil
}

func (b *S3Backend) Store(ctx context.Context, file *collector.CoredumpFile, reader io.Reader) error {
	return fmt.Errorf("S3 backend not implemented yet")
}

func (b *S3Backend) Delete(ctx context.Context, path string) error {
	return fmt.Errorf("S3 backend not implemented yet")
}

func (b *S3Backend) List(ctx context.Context) ([]*StoredFile, error) {
	return nil, fmt.Errorf("S3 backend not implemented yet")
}

func (b *S3Backend) GetStorageSize(ctx context.Context) (int64, error) {
	return 0, fmt.Errorf("S3 backend not implemented yet")
}

type NFSBackend struct {
	mountPath string
}

func NewNFSBackend(config *config.StorageConfig) (*NFSBackend, error) {
	return &NFSBackend{
		mountPath: config.LocalPath,
	}, nil
}

func (b *NFSBackend) Store(ctx context.Context, file *collector.CoredumpFile, reader io.Reader) error {
	return fmt.Errorf("NFS backend not implemented yet")
}

func (b *NFSBackend) Delete(ctx context.Context, path string) error {
	return fmt.Errorf("NFS backend not implemented yet")
}

func (b *NFSBackend) List(ctx context.Context) ([]*StoredFile, error) {
	return nil, fmt.Errorf("NFS backend not implemented yet")
}

func (b *NFSBackend) GetStorageSize(ctx context.Context) (int64, error) {
	return 0, fmt.Errorf("NFS backend not implemented yet")
}