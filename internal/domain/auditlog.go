package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AuditLogEntry represents a single audit log record.
type AuditLogEntry struct {
	ID            uuid.UUID              `json:"id"`
	Timestamp     time.Time              `json:"timestamp"`
	Actor         string                 `json:"actor"`
	Action        string                 `json:"action"`
	ResourceType  *string                `json:"resource_type,omitempty"`
	ResourceID    *uuid.UUID             `json:"resource_id,omitempty"`
	RequestMethod string                 `json:"request_method"`
	RequestPath   string                 `json:"request_path"`
	IPAddress     string                 `json:"ip_address"`
	Outcome       string                 `json:"outcome"`
	FailureReason *string                `json:"failure_reason,omitempty"`
	Metadata      map[string]interface{} `json:"metadata"`
	RequestID     *string                `json:"request_id,omitempty"`
}

// AuditLogFilter defines filter criteria for listing audit logs.
type AuditLogFilter struct {
	From         *time.Time
	To           *time.Time
	Actor        *string
	Action       *string
	ResourceType *string
	ResourceID   *uuid.UUID
	Limit        int
	Offset       int
}

// AuditLogRepository defines the interface for audit log persistence.
type AuditLogRepository interface {
	Create(ctx context.Context, entry *AuditLogEntry) error
	List(ctx context.Context, filter AuditLogFilter) ([]AuditLogEntry, int64, error)
}
