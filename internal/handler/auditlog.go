package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

// AuditLogHandler handles audit log HTTP requests.
type AuditLogHandler struct {
	auditService *service.AuditLogService
	logger       *slog.Logger
}

// NewAuditLogHandler creates a new AuditLogHandler.
func NewAuditLogHandler(
	auditService *service.AuditLogService,
	logger *slog.Logger,
) *AuditLogHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuditLogHandler{
		auditService: auditService,
		logger:       logger,
	}
}

// AuditLogListResponse is the response for GET /audit-logs.
type AuditLogListResponse struct {
	AuditLogs  []domain.AuditLogEntry `json:"audit_logs"`
	Pagination PaginationResponse     `json:"pagination"`
}

// PaginationResponse contains pagination metadata.
type PaginationResponse struct {
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

// CreateAuditEventRequest is the request body for POST /audit-events.
// All fields except event_type are optional and will use defaults if not specified.
type CreateAuditEventRequest struct {
	// Required
	EventType string `json:"event_type"`

	// Optional overrides (defaults derived from request context)
	Actor         *string                `json:"actor,omitempty"`
	ResourceType  *string                `json:"resource_type,omitempty"`
	ResourceID    *string                `json:"resource_id,omitempty"` // UUID string
	RequestMethod *string                `json:"request_method,omitempty"`
	RequestPath   *string                `json:"request_path,omitempty"`
	Outcome       *string                `json:"outcome,omitempty"`
	FailureReason *string                `json:"failure_reason,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// ServeHTTP routes audit log requests.
func (h *AuditLogHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/audit-logs":
		if r.Method == http.MethodGet {
			h.listAuditLogs(w, r)
			return
		}
	case "/audit-events":
		if r.Method == http.MethodPost {
			h.createAuditEvent(w, r)
			return
		}
	}
	WriteError(w, r, ErrInvalidRequest("method not allowed"))
}

// listAuditLogs handles GET /audit-logs.
func (h *AuditLogHandler) listAuditLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters into filter
	filter, err := h.parseAuditLogFilter(r)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest(err.Error()))
		return
	}

	entries, total, err := h.auditService.List(ctx, filter)
	if err != nil {
		WriteError(w, r, MapDomainError(err))
		return
	}

	// Ensure we return empty array instead of null
	if entries == nil {
		entries = []domain.AuditLogEntry{}
	}

	response := AuditLogListResponse{
		AuditLogs: entries,
		Pagination: PaginationResponse{
			Limit:  filter.Limit,
			Offset: filter.Offset,
			Total:  total,
		},
	}

	WriteJSON(w, http.StatusOK, response)
}

// parseAuditLogFilter parses query parameters into an AuditLogFilter.
func (h *AuditLogHandler) parseAuditLogFilter(r *http.Request) (domain.AuditLogFilter, error) {
	filter := domain.AuditLogFilter{
		Limit:  50,
		Offset: 0,
	}

	query := r.URL.Query()

	// Parse date range filters
	if fromStr := query.Get("from"); fromStr != "" {
		from, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			return filter, fmt.Errorf("invalid 'from' date format, expected RFC 3339 (e.g., 2026-01-01T00:00:00Z)")
		}
		filter.From = &from
	}

	if toStr := query.Get("to"); toStr != "" {
		to, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			return filter, fmt.Errorf("invalid 'to' date format, expected RFC 3339 (e.g., 2026-01-01T00:00:00Z)")
		}
		filter.To = &to
	}

	// Parse string filters
	if actor := query.Get("actor"); actor != "" {
		filter.Actor = &actor
	}

	if action := query.Get("action"); action != "" {
		filter.Action = &action
	}

	if resourceType := query.Get("resource_type"); resourceType != "" {
		// Resource-type vocabulary is open by design: org-admins filter the
		// audit log by whatever resource types the platform actually emits
		// (organization, team, membership, tag, device, room, …). Emission-
		// site code is the single source of truth for valid values.
		filter.ResourceType = &resourceType
	}

	if resourceIDStr := query.Get("resource_id"); resourceIDStr != "" {
		resourceID, err := uuid.Parse(resourceIDStr)
		if err != nil {
			return filter, fmt.Errorf("invalid 'resource_id' format, expected UUID")
		}
		filter.ResourceID = &resourceID
	}

	// Parse pagination parameters
	if limitStr := query.Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 || limit > 100 {
			return filter, fmt.Errorf("limit must be between 1 and 100")
		}
		filter.Limit = limit
	}

	if offsetStr := query.Get("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			return filter, fmt.Errorf("offset must be a non-negative integer")
		}
		filter.Offset = offset
	}

	return filter, nil
}

// createAuditEvent handles POST /audit-events.
func (h *AuditLogHandler) createAuditEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	apiKey := APIKeyFromContext(ctx)
	requestID := RequestIDFromContext(ctx)

	var req CreateAuditEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, ErrInvalidRequest("invalid JSON body"))
		return
	}

	// Validate event_type
	if req.EventType == "" {
		WriteError(w, r, ErrInvalidRequest("event_type is required"))
		return
	}

	if len(req.EventType) > 50 {
		WriteError(w, r, ErrInvalidRequest("event_type must be 50 characters or less"))
		return
	}

	// Validate resource_id if provided
	var resourceID *uuid.UUID
	if req.ResourceID != nil {
		parsed, err := uuid.Parse(*req.ResourceID)
		if err != nil {
			WriteError(w, r, ErrInvalidRequest("resource_id must be a valid UUID"))
			return
		}
		resourceID = &parsed
	}

	// Validate outcome if provided
	if req.Outcome != nil {
		validOutcomes := map[string]bool{"success": true, "failure": true}
		if !validOutcomes[*req.Outcome] {
			WriteError(w, r, ErrInvalidRequest("outcome must be 'success' or 'failure'"))
			return
		}
	}

	ipAddress := getClientIP(r)

	input := service.CreateCustomEventInput{
		DefaultActor:     apiKey,
		DefaultIPAddress: ipAddress,
		DefaultRequestID: requestID,
		EventType:        req.EventType,
		Actor:            req.Actor,
		ResourceType:     req.ResourceType,
		ResourceID:       resourceID,
		RequestMethod:    req.RequestMethod,
		RequestPath:      req.RequestPath,
		Outcome:          req.Outcome,
		FailureReason:    req.FailureReason,
		Metadata:         req.Metadata,
	}

	entry, err := h.auditService.CreateCustomEvent(ctx, input)
	if err != nil {
		WriteError(w, r, MapDomainError(err))
		return
	}

	WriteJSON(w, http.StatusCreated, entry)
}
