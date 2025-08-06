package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"k8s.io/klog/v2"
)

//go:embed schema.sql
var schemaSQL embed.FS

// Database represents the SQLite database connection and operations
type Database struct {
	db   *sql.DB
	path string
}

// Config holds database configuration
type Config struct {
	Path            string        `yaml:"path"`
	MaxOpenConns    int           `yaml:"maxOpenConns"`
	MaxIdleConns    int           `yaml:"maxIdleConns"`
	ConnMaxLifetime time.Duration `yaml:"connMaxLifetime"`
}

// New creates a new database connection
func New(config *Config) (*Database, error) {
	if config.Path == "" {
		config.Path = "./data/coredump_agent.db"
	}

	// Ensure directory exists
	dir := filepath.Dir(config.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", config.Path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	if config.MaxOpenConns > 0 {
		db.SetMaxOpenConns(config.MaxOpenConns)
	} else {
		db.SetMaxOpenConns(10)
	}

	if config.MaxIdleConns > 0 {
		db.SetMaxIdleConns(config.MaxIdleConns)
	} else {
		db.SetMaxIdleConns(5)
	}

	if config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(config.ConnMaxLifetime)
	} else {
		db.SetConnMaxLifetime(time.Hour)
	}

	database := &Database{
		db:   db,
		path: config.Path,
	}

	if err := database.initializeSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	klog.Infof("Database initialized at %s", config.Path)
	return database, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// GetDB returns the underlying sql.DB instance
func (d *Database) GetDB() *sql.DB {
	return d.db
}

// initializeSchema creates tables and indexes
func (d *Database) initializeSchema() error {
	schemaData, err := schemaSQL.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("failed to read schema file: %w", err)
	}

	if _, err := d.db.Exec(string(schemaData)); err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}

	return nil
}

// Ping checks if the database connection is alive
func (d *Database) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// BeginTx begins a new transaction
func (d *Database) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return d.db.BeginTx(ctx, nil)
}

// ExecuteInTransaction executes a function within a database transaction
func (d *Database) ExecuteInTransaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.BeginTx(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		} else if err != nil {
			tx.Rollback()
		} else {
			err = tx.Commit()
		}
	}()

	err = fn(tx)
	return err
}

// GetSystemStat retrieves a system statistic value
func (d *Database) GetSystemStat(ctx context.Context, statName string) (string, error) {
	var value string
	err := d.db.QueryRowContext(ctx, 
		"SELECT stat_value FROM system_stats WHERE stat_name = ?", 
		statName,
	).Scan(&value)
	
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetSystemStat updates a system statistic value
func (d *Database) SetSystemStat(ctx context.Context, statName, value string) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO system_stats (stat_name, stat_value, updated_at) 
		 VALUES (?, ?, datetime('now'))`,
		statName, value,
	)
	return err
}

// GetDatabaseStats returns basic database statistics
func (d *Database) GetDatabaseStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get table counts
	tables := []string{
		"milvus_instances", "pods", "coredump_files", 
		"analysis_results", "ai_analysis_results", 
		"restart_events", "storage_events", "cleanup_events",
	}

	for _, table := range tables {
		var count int
		err := d.db.QueryRowContext(ctx, 
			fmt.Sprintf("SELECT COUNT(*) FROM %s", table),
		).Scan(&count)
		if err != nil {
			return nil, err
		}
		stats[table+"_count"] = count
	}

	// Get database file size
	fileInfo, err := os.Stat(d.path)
	if err == nil {
		stats["database_size_bytes"] = fileInfo.Size()
	}

	return stats, nil
}

// CleanupOldRecords removes old records based on retention policies
func (d *Database) CleanupOldRecords(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		return nil
	}

	return d.ExecuteInTransaction(ctx, func(tx *sql.Tx) error {
		cutoffDate := time.Now().AddDate(0, 0, -retentionDays)

		// Clean old restart events
		_, err := tx.ExecContext(ctx,
			"DELETE FROM restart_events WHERE created_at < ?",
			cutoffDate,
		)
		if err != nil {
			return fmt.Errorf("failed to clean restart events: %w", err)
		}

		// Clean old storage events
		_, err = tx.ExecContext(ctx,
			"DELETE FROM storage_events WHERE created_at < ?",
			cutoffDate,
		)
		if err != nil {
			return fmt.Errorf("failed to clean storage events: %w", err)
		}

		// Clean old cleanup events
		_, err = tx.ExecContext(ctx,
			"DELETE FROM cleanup_events WHERE created_at < ?",
			cutoffDate,
		)
		if err != nil {
			return fmt.Errorf("failed to clean cleanup events: %w", err)
		}

		klog.Infof("Cleaned up database records older than %d days", retentionDays)
		return nil
	})
}