package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// AuditLogService handles audit log operations.
type AuditLogService struct {
	auditRepo domain.AuditLogRepository
	logger    *slog.Logger
}

// AuditLogServiceOption is a functional option for configuring AuditLogService.
type AuditLogServiceOption func(*AuditLogService)

// WithAuditLogLogger sets the logger for AuditLogService.
func WithAuditLogLogger(logger *slog.Logger) AuditLogServiceOption {
	return func(s *AuditLogService) {
		s.logger = logger
	}
}

// NewAuditLogService creates a new AuditLogService.
func NewAuditLogService(
	auditRepo domain.AuditLogRepository,
	opts ...AuditLogServiceOption,
) *AuditLogService {
	s := &AuditLogService{
		auditRepo: auditRepo,
		logger:    slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// CreateEntry creates a new audit log entry.
func (s *AuditLogService) CreateEntry(ctx context.Context, entry *domain.AuditLogEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.Metadata == nil {
		entry.Metadata = make(map[string]interface{})
	}
	if entry.Outcome == "" {
		entry.Outcome = "success"
	}

	if err := s.auditRepo.Create(ctx, entry); err != nil {
		s.logger.Error("failed to create audit log entry",
			slog.String("error", err.Error()),
			slog.String("action", entry.Action),
		)
		return err
	}

	s.logger.Debug("audit log entry created",
		slog.String("entry_id", entry.ID.String()),
		slog.String("action", entry.Action),
		slog.String("actor", entry.Actor),
	)

	return nil
}

// List retrieves audit log entries with filtering.
func (s *AuditLogService) List(ctx context.Context, filter domain.AuditLogFilter) ([]domain.AuditLogEntry, int64, error) {
	// Apply defaults
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	entries, total, err := s.auditRepo.List(ctx, filter)
	if err != nil {
		s.logger.Error("failed to list audit logs",
			slog.String("error", err.Error()),
		)
		return nil, 0, err
	}

	return entries, total, nil
}

// CreateCustomEvent creates an audit entry for a custom event.
func (s *AuditLogService) CreateCustomEvent(
	ctx context.Context,
	actor string,
	eventType string,
	metadata map[string]interface{},
	ipAddress string,
	requestID string,
) (*domain.AuditLogEntry, error) {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	var reqID *string
	if requestID != "" {
		reqID = &requestID
	}

	entry := &domain.AuditLogEntry{
		ID:            uuid.New(),
		Timestamp:     time.Now(),
		Actor:         actor,
		Action:        eventType,
		ResourceType:  nil,
		ResourceID:    nil,
		RequestMethod: "POST",
		RequestPath:   "/audit-events",
		IPAddress:     ipAddress,
		Outcome:       "success",
		Metadata:      metadata,
		RequestID:     reqID,
	}

	if err := s.auditRepo.Create(ctx, entry); err != nil {
		s.logger.Error("failed to create custom audit event",
			slog.String("error", err.Error()),
			slog.String("event_type", eventType),
			slog.String("actor", actor),
		)
		return nil, err
	}

	s.logger.Debug("custom audit event created",
		slog.String("entry_id", entry.ID.String()),
		slog.String("event_type", eventType),
		slog.String("actor", actor),
	)

	return entry, nil
}
