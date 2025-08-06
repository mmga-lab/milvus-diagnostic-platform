package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"milvus-diagnostic-platform/pkg/collector"
)

// CoredumpStore handles persistence of coredump file data
type CoredumpStore struct {
	db *Database
}

// NewCoredumpStore creates a new coredump store
func NewCoredumpStore(db *Database) *CoredumpStore {
	return &CoredumpStore{db: db}
}

// SaveCoredumpFile saves a coredump file to the database
func (cs *CoredumpStore) SaveCoredumpFile(ctx context.Context, file *collector.CoredumpFile) error {
	return cs.db.ExecuteInTransaction(ctx, func(tx *sql.Tx) error {
		// Encode arguments as JSON
		argumentsJSON, err := json.Marshal(file.Arguments)
		if err != nil {
			argumentsJSON = []byte("[]")
		}

		// Insert or update coredump file
		_, err = tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO coredump_files (
				path, file_name, size, mod_time, pid, uid, gid, signal,
				timestamp, executable, arguments, hostname,
				pod_name, pod_namespace, container_name, instance_name,
				is_analyzed, value_score, analysis_time,
				status, error_message, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
			file.Path, file.FileName, file.Size, file.ModTime, file.PID, file.UID, file.GID, file.Signal,
			file.Timestamp, file.Executable, string(argumentsJSON), file.Hostname,
			file.PodName, file.PodNamespace, file.ContainerName, file.InstanceName,
			file.IsAnalyzed, file.ValueScore, file.AnalysisTime,
			string(file.Status), file.ErrorMessage,
		)
		if err != nil {
			return fmt.Errorf("failed to save coredump file: %w", err)
		}

		// Get the file ID for saving analysis results
		var fileID int64
		err = tx.QueryRowContext(ctx, "SELECT id FROM coredump_files WHERE path = ?", file.Path).Scan(&fileID)
		if err != nil {
			return fmt.Errorf("failed to get coredump file ID: %w", err)
		}

		// Save analysis results if available
		if file.AnalysisResults != nil {
			err = cs.saveAnalysisResults(ctx, tx, fileID, file.AnalysisResults)
			if err != nil {
				return fmt.Errorf("failed to save analysis results: %w", err)
			}
		}

		return nil
	})
}

// LoadCoredumpFiles loads all coredump files from the database
func (cs *CoredumpStore) LoadCoredumpFiles(ctx context.Context) ([]*collector.CoredumpFile, error) {
	rows, err := cs.db.db.QueryContext(ctx, `
		SELECT 
			path, file_name, size, mod_time, pid, uid, gid, signal,
			timestamp, executable, arguments, hostname,
			pod_name, pod_namespace, container_name, instance_name,
			is_analyzed, value_score, analysis_time,
			status, error_message, created_at, updated_at
		FROM coredump_files
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query coredump files: %w", err)
	}
	defer rows.Close()

	var files []*collector.CoredumpFile
	for rows.Next() {
		file, err := cs.scanCoredumpFile(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan coredump file: %w", err)
		}

		// Load analysis results
		analysisResults, err := cs.loadAnalysisResults(ctx, file.Path)
		if err != nil {
			// Log error but continue
			continue
		}
		file.AnalysisResults = analysisResults

		files = append(files, file)
	}

	return files, nil
}

// LoadCoredumpFile loads a single coredump file by path
func (cs *CoredumpStore) LoadCoredumpFile(ctx context.Context, path string) (*collector.CoredumpFile, error) {
	row := cs.db.db.QueryRowContext(ctx, `
		SELECT 
			path, file_name, size, mod_time, pid, uid, gid, signal,
			timestamp, executable, arguments, hostname,
			pod_name, pod_namespace, container_name, instance_name,
			is_analyzed, value_score, analysis_time,
			status, error_message, created_at, updated_at
		FROM coredump_files
		WHERE path = ?
	`, path)

	file, err := cs.scanCoredumpFile(row)
	if err != nil {
		return nil, fmt.Errorf("failed to load coredump file: %w", err)
	}

	// Load analysis results
	analysisResults, err := cs.loadAnalysisResults(ctx, path)
	if err == nil {
		file.AnalysisResults = analysisResults
	}

	return file, nil
}

// GetCoredumpsByInstance gets coredump files for a specific instance
func (cs *CoredumpStore) GetCoredumpsByInstance(ctx context.Context, instanceName string) ([]*collector.CoredumpFile, error) {
	rows, err := cs.db.db.QueryContext(ctx, `
		SELECT 
			path, file_name, size, mod_time, pid, uid, gid, signal,
			timestamp, executable, arguments, hostname,
			pod_name, pod_namespace, container_name, instance_name,
			is_analyzed, value_score, analysis_time,
			status, error_message, created_at, updated_at
		FROM coredump_files
		WHERE instance_name = ?
		ORDER BY created_at DESC
	`, instanceName)
	if err != nil {
		return nil, fmt.Errorf("failed to query coredump files by instance: %w", err)
	}
	defer rows.Close()

	var files []*collector.CoredumpFile
	for rows.Next() {
		file, err := cs.scanCoredumpFile(rows)
		if err != nil {
			continue // Skip problematic records
		}
		files = append(files, file)
	}

	return files, nil
}

// GetTodayProcessedCount gets count of coredumps processed today
func (cs *CoredumpStore) GetTodayProcessedCount(ctx context.Context) (int, error) {
	today := time.Now().Truncate(24 * time.Hour)
	
	var count int
	err := cs.db.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM coredump_files WHERE analysis_time >= ? AND is_analyzed = true",
		today,
	).Scan(&count)
	
	return count, err
}

// GetHighValueCount gets count of high-value coredumps (score >= 7.0)
func (cs *CoredumpStore) GetHighValueCount(ctx context.Context) (int, error) {
	var count int
	err := cs.db.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM coredump_files WHERE value_score >= 7.0",
	).Scan(&count)
	
	return count, err
}

// DeleteCoredumpFile deletes a coredump file record
func (cs *CoredumpStore) DeleteCoredumpFile(ctx context.Context, path string) error {
	_, err := cs.db.db.ExecContext(ctx, "DELETE FROM coredump_files WHERE path = ?", path)
	return err
}

// scanCoredumpFile scans a database row into a CoredumpFile struct
func (cs *CoredumpStore) scanCoredumpFile(scanner interface {
	Scan(dest ...interface{}) error
}) (*collector.CoredumpFile, error) {
	file := &collector.CoredumpFile{}
	var argumentsJSON string
	var modTime, timestamp, analysisTime sql.NullTime
	var createdAt, updatedAt time.Time
	var podName, podNamespace, containerName, instanceName, errorMessage sql.NullString

	err := scanner.Scan(
		&file.Path, &file.FileName, &file.Size, &modTime, &file.PID, &file.UID, &file.GID, &file.Signal,
		&timestamp, &file.Executable, &argumentsJSON, &file.Hostname,
		&podName, &podNamespace, &containerName, &instanceName,
		&file.IsAnalyzed, &file.ValueScore, &analysisTime,
		&file.Status, &errorMessage, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Handle nullable fields
	if modTime.Valid {
		file.ModTime = modTime.Time
	}
	if timestamp.Valid {
		file.Timestamp = timestamp.Time
	}
	if analysisTime.Valid {
		file.AnalysisTime = analysisTime.Time
	}
	if podName.Valid {
		file.PodName = podName.String
	}
	if podNamespace.Valid {
		file.PodNamespace = podNamespace.String
	}
	if containerName.Valid {
		file.ContainerName = containerName.String
	}
	if instanceName.Valid {
		file.InstanceName = instanceName.String
	}
	if errorMessage.Valid {
		file.ErrorMessage = errorMessage.String
	}

	// Parse arguments JSON
	if argumentsJSON != "" {
		json.Unmarshal([]byte(argumentsJSON), &file.Arguments)
	}

	// Convert timestamps
	file.CreatedAt.Time = createdAt
	file.UpdatedAt.Time = updatedAt

	return file, nil
}

// saveAnalysisResults saves analysis results to the database
func (cs *CoredumpStore) saveAnalysisResults(ctx context.Context, tx *sql.Tx, fileID int64, results *collector.AnalysisResults) error {
	// Encode JSON fields
	libraryVersionsJSON, _ := json.Marshal(results.LibraryVersions)
	memoryInfoJSON, _ := json.Marshal(results.MemoryInfo)
	registerInfoJSON, _ := json.Marshal(results.RegisterInfo)
	sharedLibrariesJSON, _ := json.Marshal(results.SharedLibraries)

	// Insert analysis results
	_, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO analysis_results (
			coredump_file_id, stack_trace, crash_reason, crash_address, thread_count,
			library_versions, memory_info, register_info, shared_libraries
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fileID, results.StackTrace, results.CrashReason, results.CrashAddress, results.ThreadCount,
		string(libraryVersionsJSON), string(memoryInfoJSON), 
		string(registerInfoJSON), string(sharedLibrariesJSON),
	)
	if err != nil {
		return err
	}

	// Save AI analysis results if available
	if results.AIAnalysis != nil {
		err = cs.saveAIAnalysisResults(ctx, tx, fileID, results.AIAnalysis)
		if err != nil {
			return err
		}
	}

	return nil
}

// saveAIAnalysisResults saves AI analysis results to the database
func (cs *CoredumpStore) saveAIAnalysisResults(ctx context.Context, tx *sql.Tx, fileID int64, ai *collector.AIAnalysisResult) error {
	recommendationsJSON, _ := json.Marshal(ai.Recommendations)
	relatedIssuesJSON, _ := json.Marshal(ai.RelatedIssues)
	codeSuggestionsJSON, _ := json.Marshal(ai.CodeSuggestions)

	_, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO ai_analysis_results (
			coredump_file_id, enabled, provider, model, analysis_time,
			summary, root_cause, impact, recommendations, confidence,
			tokens_used, cost_usd, error_message, related_issues, code_suggestions
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fileID, ai.Enabled, ai.Provider, ai.Model, ai.AnalysisTime,
		ai.Summary, ai.RootCause, ai.Impact, string(recommendationsJSON), ai.Confidence,
		ai.TokensUsed, ai.CostUSD, ai.ErrorMessage, 
		string(relatedIssuesJSON), string(codeSuggestionsJSON),
	)

	return err
}

// loadAnalysisResults loads analysis results for a coredump file
func (cs *CoredumpStore) loadAnalysisResults(ctx context.Context, filePath string) (*collector.AnalysisResults, error) {
	var results collector.AnalysisResults
	var libraryVersionsJSON, memoryInfoJSON, registerInfoJSON, sharedLibrariesJSON string

	err := cs.db.db.QueryRowContext(ctx, `
		SELECT ar.stack_trace, ar.crash_reason, ar.crash_address, ar.thread_count,
			   ar.library_versions, ar.memory_info, ar.register_info, ar.shared_libraries
		FROM analysis_results ar
		JOIN coredump_files cf ON ar.coredump_file_id = cf.id
		WHERE cf.path = ?`,
		filePath,
	).Scan(
		&results.StackTrace, &results.CrashReason, &results.CrashAddress, &results.ThreadCount,
		&libraryVersionsJSON, &memoryInfoJSON, &registerInfoJSON, &sharedLibrariesJSON,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Parse JSON fields
	json.Unmarshal([]byte(libraryVersionsJSON), &results.LibraryVersions)
	json.Unmarshal([]byte(memoryInfoJSON), &results.MemoryInfo)
	json.Unmarshal([]byte(registerInfoJSON), &results.RegisterInfo)
	json.Unmarshal([]byte(sharedLibrariesJSON), &results.SharedLibraries)

	// Load AI analysis results
	aiResults, err := cs.loadAIAnalysisResults(ctx, filePath)
	if err == nil && aiResults != nil {
		results.AIAnalysis = aiResults
	}

	return &results, nil
}

// loadAIAnalysisResults loads AI analysis results for a coredump file
func (cs *CoredumpStore) loadAIAnalysisResults(ctx context.Context, filePath string) (*collector.AIAnalysisResult, error) {
	var ai collector.AIAnalysisResult
	var recommendationsJSON, relatedIssuesJSON, codeSuggestionsJSON string
	var errorMessage sql.NullString

	err := cs.db.db.QueryRowContext(ctx, `
		SELECT ai.enabled, ai.provider, ai.model, ai.analysis_time,
			   ai.summary, ai.root_cause, ai.impact, ai.recommendations, ai.confidence,
			   ai.tokens_used, ai.cost_usd, ai.error_message, ai.related_issues, ai.code_suggestions
		FROM ai_analysis_results ai
		JOIN coredump_files cf ON ai.coredump_file_id = cf.id
		WHERE cf.path = ?`,
		filePath,
	).Scan(
		&ai.Enabled, &ai.Provider, &ai.Model, &ai.AnalysisTime,
		&ai.Summary, &ai.RootCause, &ai.Impact, &recommendationsJSON, &ai.Confidence,
		&ai.TokensUsed, &ai.CostUSD, &errorMessage, &relatedIssuesJSON, &codeSuggestionsJSON,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Handle nullable error message
	if errorMessage.Valid {
		ai.ErrorMessage = errorMessage.String
	}

	// Parse JSON fields
	json.Unmarshal([]byte(recommendationsJSON), &ai.Recommendations)
	json.Unmarshal([]byte(relatedIssuesJSON), &ai.RelatedIssues)
	json.Unmarshal([]byte(codeSuggestionsJSON), &ai.CodeSuggestions)

	return &ai, nil
}