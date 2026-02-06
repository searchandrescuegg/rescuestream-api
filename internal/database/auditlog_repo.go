package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// AuditLogRepo implements domain.AuditLogRepository using pgxpool.
type AuditLogRepo struct {
	pool *pgxpool.Pool
}

// NewAuditLogRepo creates a new AuditLogRepo.
func NewAuditLogRepo(pool *pgxpool.Pool) *AuditLogRepo {
	return &AuditLogRepo{pool: pool}
}

// Create creates a new audit log entry.
func (r *AuditLogRepo) Create(ctx context.Context, entry *domain.AuditLogEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}

	metadataJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO audit_logs (
			id, timestamp, actor, action, resource_type, resource_id,
			request_method, request_path, ip_address, outcome,
			failure_reason, metadata, request_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err = r.pool.Exec(ctx, query,
		entry.ID,
		entry.Timestamp,
		entry.Actor,
		entry.Action,
		entry.ResourceType,
		entry.ResourceID,
		entry.RequestMethod,
		entry.RequestPath,
		entry.IPAddress,
		entry.Outcome,
		entry.FailureReason,
		metadataJSON,
		entry.RequestID,
	)
	if err != nil {
		return fmt.Errorf("failed to create audit log entry: %w", err)
	}

	return nil
}

// List retrieves audit log entries with filtering and pagination.
func (r *AuditLogRepo) List(ctx context.Context, filter domain.AuditLogFilter) ([]domain.AuditLogEntry, int64, error) {
	// Build dynamic WHERE clause
	var conditions []string
	var args []interface{}
	argNum := 1

	if filter.From != nil {
		conditions = append(conditions, fmt.Sprintf("timestamp >= $%d", argNum))
		args = append(args, *filter.From)
		argNum++
	}
	if filter.To != nil {
		conditions = append(conditions, fmt.Sprintf("timestamp < $%d", argNum))
		args = append(args, *filter.To)
		argNum++
	}
	if filter.Actor != nil {
		conditions = append(conditions, fmt.Sprintf("actor = $%d", argNum))
		args = append(args, *filter.Actor)
		argNum++
	}
	if filter.Action != nil {
		conditions = append(conditions, fmt.Sprintf("action = $%d", argNum))
		args = append(args, *filter.Action)
		argNum++
	}
	if filter.ResourceType != nil {
		conditions = append(conditions, fmt.Sprintf("resource_type = $%d", argNum))
		args = append(args, *filter.ResourceType)
		argNum++
	}
	if filter.ResourceID != nil {
		conditions = append(conditions, fmt.Sprintf("resource_id = $%d", argNum))
		args = append(args, *filter.ResourceID)
		argNum++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_logs %s", whereClause)
	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

	// Data query with pagination
	dataQuery := fmt.Sprintf(`
		SELECT id, timestamp, actor, action, resource_type, resource_id,
			   request_method, request_path, ip_address, outcome,
			   failure_reason, metadata, request_id
		FROM audit_logs
		%s
		ORDER BY timestamp DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argNum, argNum+1)

	args = append(args, filter.Limit, filter.Offset)

	rows, err := r.pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var entries []domain.AuditLogEntry
	for rows.Next() {
		var entry domain.AuditLogEntry
		var metadataJSON []byte

		err := rows.Scan(
			&entry.ID,
			&entry.Timestamp,
			&entry.Actor,
			&entry.Action,
			&entry.ResourceType,
			&entry.ResourceID,
			&entry.RequestMethod,
			&entry.RequestPath,
			&entry.IPAddress,
			&entry.Outcome,
			&entry.FailureReason,
			&metadataJSON,
			&entry.RequestID,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan audit log: %w", err)
		}

		if err := json.Unmarshal(metadataJSON, &entry.Metadata); err != nil {
			return nil, 0, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating audit logs: %w", err)
	}

	return entries, total, nil
}
