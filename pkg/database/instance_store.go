package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"milvus-diagnostic-platform/pkg/discovery"
)

// InstanceStore handles persistence of Milvus instance data
type InstanceStore struct {
	db *Database
}

// NewInstanceStore creates a new instance store
func NewInstanceStore(db *Database) *InstanceStore {
	return &InstanceStore{db: db}
}

// SaveInstance saves a Milvus instance to the database
func (is *InstanceStore) SaveInstance(ctx context.Context, instance *discovery.MilvusInstance) error {
	return is.db.ExecuteInTransaction(ctx, func(tx *sql.Tx) error {
		// Encode labels as JSON
		labelsJSON, err := json.Marshal(instance.Labels)
		if err != nil {
			labelsJSON = []byte("{}")
		}

		// Insert or update instance
		_, err = tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO milvus_instances (
				name, namespace, deployment_type, labels, status, updated_at, last_seen
			) VALUES (?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
			instance.Name, instance.Namespace, string(instance.Type), 
			string(labelsJSON), "active",
		)
		if err != nil {
			return fmt.Errorf("failed to save instance: %w", err)
		}

		// Get instance ID
		var instanceID int64
		err = tx.QueryRowContext(ctx, 
			"SELECT id FROM milvus_instances WHERE name = ? AND namespace = ?",
			instance.Name, instance.Namespace,
		).Scan(&instanceID)
		if err != nil {
			return fmt.Errorf("failed to get instance ID: %w", err)
		}

		// Save pods
		for _, pod := range instance.Pods {
			err = is.savePod(ctx, tx, instanceID, &pod)
			if err != nil {
				return fmt.Errorf("failed to save pod %s: %w", pod.Name, err)
			}
		}

		return nil
	})
}

// LoadInstances loads all instances from the database
func (is *InstanceStore) LoadInstances(ctx context.Context) ([]*discovery.MilvusInstance, error) {
	rows, err := is.db.db.QueryContext(ctx, `
		SELECT name, namespace, deployment_type, labels, status, created_at, updated_at, last_seen
		FROM milvus_instances
		WHERE last_seen > datetime('now', '-1 hour')
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query instances: %w", err)
	}
	defer rows.Close()

	var instances []*discovery.MilvusInstance
	for rows.Next() {
		instance, err := is.scanInstance(rows)
		if err != nil {
			continue // Skip problematic records
		}

		// Load pods for this instance
		pods, err := is.loadPodsForInstance(ctx, instance.Name, instance.Namespace)
		if err == nil {
			instance.Pods = pods
		}

		instances = append(instances, instance)
	}

	return instances, nil
}

// LoadInstance loads a single instance by name and namespace
func (is *InstanceStore) LoadInstance(ctx context.Context, name, namespace string) (*discovery.MilvusInstance, error) {
	row := is.db.db.QueryRowContext(ctx, `
		SELECT name, namespace, deployment_type, labels, status, created_at, updated_at, last_seen
		FROM milvus_instances
		WHERE name = ? AND namespace = ?
	`, name, namespace)

	instance, err := is.scanInstance(row)
	if err != nil {
		return nil, fmt.Errorf("failed to load instance: %w", err)
	}

	// Load pods
	pods, err := is.loadPodsForInstance(ctx, name, namespace)
	if err == nil {
		instance.Pods = pods
	}

	return instance, nil
}

// UpdateInstanceLastSeen updates the last seen timestamp for an instance
func (is *InstanceStore) UpdateInstanceLastSeen(ctx context.Context, name, namespace string) error {
	_, err := is.db.db.ExecContext(ctx,
		"UPDATE milvus_instances SET last_seen = datetime('now') WHERE name = ? AND namespace = ?",
		name, namespace,
	)
	return err
}

// DeleteStaleInstances removes instances that haven't been seen recently
func (is *InstanceStore) DeleteStaleInstances(ctx context.Context, staleThreshold time.Duration) error {
	cutoffTime := time.Now().Add(-staleThreshold)
	_, err := is.db.db.ExecContext(ctx,
		"DELETE FROM milvus_instances WHERE last_seen < ?",
		cutoffTime,
	)
	return err
}

// GetInstanceCount gets the total number of active instances
func (is *InstanceStore) GetInstanceCount(ctx context.Context) (int, error) {
	var count int
	err := is.db.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM milvus_instances WHERE last_seen > datetime('now', '-1 hour')",
	).Scan(&count)
	return count, err
}

// SaveRestartEvent saves a pod restart event
func (is *InstanceStore) SaveRestartEvent(ctx context.Context, event *discovery.RestartEvent) error {
	_, err := is.db.db.ExecContext(ctx, `
		INSERT INTO restart_events (
			pod_name, pod_namespace, container_name, restart_time,
			exit_code, is_panic, reason, message, instance_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.PodName, event.PodNamespace, event.ContainerName, event.RestartTime.Time,
		event.ExitCode, event.IsPanic, event.Reason, event.Message, event.InstanceName,
	)
	return err
}

// GetRecentRestartEvents gets restart events within a time window
func (is *InstanceStore) GetRecentRestartEvents(ctx context.Context, since time.Duration) ([]*discovery.RestartEvent, error) {
	cutoffTime := time.Now().Add(-since)
	
	rows, err := is.db.db.QueryContext(ctx, `
		SELECT pod_name, pod_namespace, container_name, restart_time,
			   exit_code, is_panic, reason, message, instance_name
		FROM restart_events
		WHERE restart_time > ?
		ORDER BY restart_time DESC`,
		cutoffTime,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*discovery.RestartEvent
	for rows.Next() {
		event, err := is.scanRestartEvent(rows)
		if err != nil {
			continue
		}
		events = append(events, event)
	}

	return events, nil
}

// scanInstance scans a database row into a MilvusInstance struct
func (is *InstanceStore) scanInstance(scanner interface {
	Scan(dest ...interface{}) error
}) (*discovery.MilvusInstance, error) {
	instance := &discovery.MilvusInstance{}
	var deploymentType, labelsJSON, status string
	var createdAt, updatedAt, lastSeen time.Time

	err := scanner.Scan(
		&instance.Name, &instance.Namespace, &deploymentType, &labelsJSON, &status,
		&createdAt, &updatedAt, &lastSeen,
	)
	if err != nil {
		return nil, err
	}

	// Convert deployment type
	instance.Type = discovery.DeploymentType(deploymentType)

	// Parse labels JSON
	if labelsJSON != "" {
		json.Unmarshal([]byte(labelsJSON), &instance.Labels)
	}

	return instance, nil
}

// savePod saves a pod to the database
func (is *InstanceStore) savePod(ctx context.Context, tx *sql.Tx, instanceID int64, pod *discovery.PodInfo) error {
	_, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO pods (
			instance_id, name, namespace, restart_count, last_restart, status, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, datetime('now'))`,
		instanceID, pod.Name, pod.Namespace, pod.RestartCount, 
		pod.LastRestart.Time, pod.Status,
	)
	return err
}

// loadPodsForInstance loads all pods for a specific instance
func (is *InstanceStore) loadPodsForInstance(ctx context.Context, instanceName, instanceNamespace string) ([]discovery.PodInfo, error) {
	rows, err := is.db.db.QueryContext(ctx, `
		SELECT p.name, p.namespace, p.restart_count, p.last_restart, p.status
		FROM pods p
		JOIN milvus_instances mi ON p.instance_id = mi.id
		WHERE mi.name = ? AND mi.namespace = ?
		ORDER BY p.name`,
		instanceName, instanceNamespace,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pods []discovery.PodInfo
	for rows.Next() {
		var pod discovery.PodInfo
		var lastRestart sql.NullTime
		
		err := rows.Scan(
			&pod.Name, &pod.Namespace, &pod.RestartCount, &lastRestart, &pod.Status,
		)
		if err != nil {
			continue
		}

		if lastRestart.Valid {
			pod.LastRestart.Time = lastRestart.Time
		}

		pods = append(pods, pod)
	}

	return pods, nil
}

// scanRestartEvent scans a database row into a RestartEvent struct
func (is *InstanceStore) scanRestartEvent(scanner interface {
	Scan(dest ...interface{}) error
}) (*discovery.RestartEvent, error) {
	event := &discovery.RestartEvent{}
	var restartTime time.Time
	var reason, message, instanceName sql.NullString

	err := scanner.Scan(
		&event.PodName, &event.PodNamespace, &event.ContainerName, &restartTime,
		&event.ExitCode, &event.IsPanic, &reason, &message, &instanceName,
	)
	if err != nil {
		return nil, err
	}

	event.RestartTime.Time = restartTime

	if reason.Valid {
		event.Reason = reason.String
	}
	if message.Valid {
		event.Message = message.String
	}
	if instanceName.Valid {
		event.InstanceName = instanceName.String
	}

	return event, nil
}