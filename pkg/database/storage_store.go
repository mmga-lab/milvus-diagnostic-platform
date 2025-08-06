package database

import (
	"context"
	"database/sql"

	"milvus-diagnostic-platform/pkg/storage"
)

// StorageStore handles persistence of storage-related events and operations
type StorageStore struct {
	db *Database
}

// NewStorageStore creates a new storage store
func NewStorageStore(db *Database) *StorageStore {
	return &StorageStore{db: db}
}

// SaveStorageEvent saves a storage event to the database
func (ss *StorageStore) SaveStorageEvent(ctx context.Context, event *storage.StorageEvent) error {
	var coredumpFileID sql.NullInt64
	var storagePath sql.NullString
	var fileSize, compressedSize sql.NullInt64

	// Get coredump file ID if available
	if event.CoredumpFile != nil {
		var id int64
		err := ss.db.db.QueryRowContext(ctx,
			"SELECT id FROM coredump_files WHERE path = ?",
			event.CoredumpFile.Path,
		).Scan(&id)
		if err == nil {
			coredumpFileID.Int64 = id
			coredumpFileID.Valid = true
			
			// Set file size information
			fileSize.Int64 = event.CoredumpFile.Size
			fileSize.Valid = true
		}
	}

	_, err := ss.db.db.ExecContext(ctx, `
		INSERT INTO storage_events (
			coredump_file_id, event_type, backend_type, storage_path,
			file_size, compressed_size, error_message
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		coredumpFileID, string(event.Type), "local", storagePath,
		fileSize, compressedSize, event.Error,
	)

	return err
}

// GetStorageEventsByFile gets all storage events for a specific coredump file
func (ss *StorageStore) GetStorageEventsByFile(ctx context.Context, filePath string) ([]StorageEventRecord, error) {
	rows, err := ss.db.db.QueryContext(ctx, `
		SELECT se.event_type, se.backend_type, se.storage_path, se.file_size,
			   se.compressed_size, se.error_message, se.created_at
		FROM storage_events se
		JOIN coredump_files cf ON se.coredump_file_id = cf.id
		WHERE cf.path = ?
		ORDER BY se.created_at DESC`,
		filePath,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []StorageEventRecord
	for rows.Next() {
		var event StorageEventRecord
		var storagePath, errorMessage sql.NullString
		var fileSize, compressedSize sql.NullInt64

		err := rows.Scan(
			&event.EventType, &event.BackendType, &storagePath,
			&fileSize, &compressedSize, &errorMessage, &event.CreatedAt,
		)
		if err != nil {
			continue
		}

		if storagePath.Valid {
			event.StoragePath = storagePath.String
		}
		if errorMessage.Valid {
			event.ErrorMessage = errorMessage.String
		}
		if fileSize.Valid {
			event.FileSize = fileSize.Int64
		}
		if compressedSize.Valid {
			event.CompressedSize = compressedSize.Int64
		}

		events = append(events, event)
	}

	return events, nil
}

// GetStorageStats gets storage statistics
func (ss *StorageStore) GetStorageStats(ctx context.Context) (*StorageStats, error) {
	stats := &StorageStats{}

	// Get total storage events
	err := ss.db.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM storage_events WHERE event_type = 'file_stored'",
	).Scan(&stats.TotalFilesStored)
	if err != nil {
		return nil, err
	}

	// Get total storage errors
	err = ss.db.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM storage_events WHERE event_type = 'storage_error'",
	).Scan(&stats.TotalStorageErrors)
	if err != nil {
		return nil, err
	}

	// Get total bytes stored (approximation)
	var totalSize sql.NullInt64
	err = ss.db.db.QueryRowContext(ctx,
		`SELECT SUM(COALESCE(compressed_size, file_size)) 
		 FROM storage_events 
		 WHERE event_type = 'file_stored' AND coredump_file_id IS NOT NULL`,
	).Scan(&totalSize)
	if err == nil && totalSize.Valid {
		stats.TotalBytesStored = totalSize.Int64
	}

	return stats, nil
}

// SaveCleanupEvent saves a cleanup event to the database
func (ss *StorageStore) SaveCleanupEvent(ctx context.Context, instanceName, namespace, cleanupType, reason string, success bool, errorMessage string, restartCount int) error {
	_, err := ss.db.db.ExecContext(ctx, `
		INSERT INTO cleanup_events (
			instance_name, namespace, cleanup_type, restart_count, reason,
			success, error_message
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		instanceName, namespace, cleanupType, restartCount, reason,
		success, errorMessage,
	)
	return err
}

// GetCleanupEvents gets recent cleanup events
func (ss *StorageStore) GetCleanupEvents(ctx context.Context, limit int) ([]CleanupEventRecord, error) {
	rows, err := ss.db.db.QueryContext(ctx, `
		SELECT instance_name, namespace, cleanup_type, restart_count, reason,
			   success, error_message, created_at
		FROM cleanup_events
		ORDER BY created_at DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []CleanupEventRecord
	for rows.Next() {
		var event CleanupEventRecord
		var reason, errorMessage sql.NullString

		err := rows.Scan(
			&event.InstanceName, &event.Namespace, &event.CleanupType, &event.RestartCount,
			&reason, &event.Success, &errorMessage, &event.CreatedAt,
		)
		if err != nil {
			continue
		}

		if reason.Valid {
			event.Reason = reason.String
		}
		if errorMessage.Valid {
			event.ErrorMessage = errorMessage.String
		}

		events = append(events, event)
	}

	return events, nil
}

// GetCleanupStats gets cleanup statistics
func (ss *StorageStore) GetCleanupStats(ctx context.Context) (*CleanupStats, error) {
	stats := &CleanupStats{}

	// Get total cleanups
	err := ss.db.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM cleanup_events",
	).Scan(&stats.TotalCleanups)
	if err != nil {
		return nil, err
	}

	// Get successful cleanups
	err = ss.db.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM cleanup_events WHERE success = true",
	).Scan(&stats.SuccessfulCleanups)
	if err != nil {
		return nil, err
	}

	// Get failed cleanups
	err = ss.db.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM cleanup_events WHERE success = false",
	).Scan(&stats.FailedCleanups)
	if err != nil {
		return nil, err
	}

	return stats, nil
}

// StorageEventRecord represents a storage event record
type StorageEventRecord struct {
	EventType      string `json:"eventType"`
	BackendType    string `json:"backendType"`
	StoragePath    string `json:"storagePath,omitempty"`
	FileSize       int64  `json:"fileSize,omitempty"`
	CompressedSize int64  `json:"compressedSize,omitempty"`
	ErrorMessage   string `json:"errorMessage,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

// StorageStats represents storage statistics
type StorageStats struct {
	TotalFilesStored   int   `json:"totalFilesStored"`
	TotalStorageErrors int   `json:"totalStorageErrors"`
	TotalBytesStored   int64 `json:"totalBytesStored"`
}

// CleanupEventRecord represents a cleanup event record
type CleanupEventRecord struct {
	InstanceName string `json:"instanceName"`
	Namespace    string `json:"namespace"`
	CleanupType  string `json:"cleanupType"`
	RestartCount int    `json:"restartCount"`
	Reason       string `json:"reason,omitempty"`
	Success      bool   `json:"success"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	CreatedAt    string `json:"createdAt"`
}

// CleanupStats represents cleanup statistics
type CleanupStats struct {
	TotalCleanups      int `json:"totalCleanups"`
	SuccessfulCleanups int `json:"successfulCleanups"`
	FailedCleanups     int `json:"failedCleanups"`
}