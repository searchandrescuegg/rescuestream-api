package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// WebhookHandler handles MediaMTX lifecycle webhooks.
type WebhookHandler struct {
	streamRepo    domain.StreamRepository
	streamKeyRepo domain.StreamKeyRepository
	logger        *slog.Logger
}

// NewWebhookHandler creates a new WebhookHandler.
func NewWebhookHandler(
	streamRepo domain.StreamRepository,
	streamKeyRepo domain.StreamKeyRepository,
	logger *slog.Logger,
) *WebhookHandler {
	return &WebhookHandler{
		streamRepo:    streamRepo,
		streamKeyRepo: streamKeyRepo,
		logger:        logger,
	}
}

// WebhookReadyRequest represents the request body for stream ready webhook.
type WebhookReadyRequest struct {
	Path       string `json:"path"`
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
}

// WebhookNotReadyRequest represents the request body for stream not ready webhook.
type WebhookNotReadyRequest struct {
	Path string `json:"path"`
}

// ServeHTTP routes webhook requests to the appropriate handler.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, r, ErrInvalidRequest("method not allowed"))
		return
	}

	// Get the route from the URL path
	route := mux.CurrentRoute(r)
	if route == nil {
		// Fallback to checking URL path directly
		if strings.HasSuffix(r.URL.Path, "/ready") {
			h.handleReady(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/not-ready") {
			h.handleNotReady(w, r)
		} else {
			WriteError(w, r, ErrNotFound("unknown webhook endpoint"))
		}
		return
	}

	path, err := route.GetPathTemplate()
	if err != nil {
		WriteError(w, r, ErrInternalServer("failed to get route template"))
		return
	}
	if strings.HasSuffix(path, "/ready") {
		h.handleReady(w, r)
	} else if strings.HasSuffix(path, "/not-ready") {
		h.handleNotReady(w, r)
	} else {
		WriteError(w, r, ErrNotFound("unknown webhook endpoint"))
	}
}

func (h *WebhookHandler) handleReady(w http.ResponseWriter, r *http.Request) {
	var req WebhookReadyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("failed to decode webhook ready request",
			slog.String("error", err.Error()),
		)
		WriteError(w, r, ErrInvalidRequest("invalid request body"))
		return
	}

	if req.Path == "" {
		WriteError(w, r, ErrInvalidRequest("path is required"))
		return
	}

	h.logger.Info("stream ready webhook received",
		slog.String("path", req.Path),
		slog.String("source_type", req.SourceType),
		slog.String("source_id", req.SourceID),
	)

	// The path is the stream key value in our system
	// Look up the stream key by its value
	streamKey, err := h.streamKeyRepo.GetByKeyValue(r.Context(), req.Path)
	if err != nil {
		h.logger.Warn("stream key not found for path",
			slog.String("path", req.Path),
			slog.String("error", err.Error()),
		)
		// Don't fail the webhook - MediaMTX needs 2xx to continue
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Create stream record
	stream := &domain.Stream{
		ID:          uuid.New(),
		StreamKeyID: streamKey.ID,
		Path:        req.Path,
		Status:      domain.StreamStatusActive,
		StartedAt:   time.Now(),
		Metadata:    make(map[string]interface{}),
	}

	if req.SourceType != "" {
		stream.SourceType = &req.SourceType
	}
	if req.SourceID != "" {
		stream.SourceID = &req.SourceID
	}

	if err := h.streamRepo.Create(r.Context(), stream); err != nil {
		h.logger.Error("failed to create stream record",
			slog.String("error", err.Error()),
			slog.String("path", req.Path),
		)
		// Don't fail the webhook - MediaMTX needs 2xx to continue
		w.WriteHeader(http.StatusNoContent)
		return
	}

	h.logger.Info("stream started",
		slog.String("stream_id", stream.ID.String()),
		slog.String("stream_key_id", streamKey.ID.String()),
		slog.String("path", req.Path),
	)

	w.WriteHeader(http.StatusNoContent)
}

func (h *WebhookHandler) handleNotReady(w http.ResponseWriter, r *http.Request) {
	var req WebhookNotReadyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("failed to decode webhook not-ready request",
			slog.String("error", err.Error()),
		)
		WriteError(w, r, ErrInvalidRequest("invalid request body"))
		return
	}

	if req.Path == "" {
		WriteError(w, r, ErrInvalidRequest("path is required"))
		return
	}

	h.logger.Info("stream not-ready webhook received",
		slog.String("path", req.Path),
	)

	// End the stream by path
	if err := h.streamRepo.EndStreamByPath(r.Context(), req.Path); err != nil {
		if err != domain.ErrNotFound {
			h.logger.Error("failed to end stream",
				slog.String("error", err.Error()),
				slog.String("path", req.Path),
			)
		} else {
			h.logger.Warn("no active stream found for path",
				slog.String("path", req.Path),
			)
		}
	} else {
		h.logger.Info("stream ended",
			slog.String("path", req.Path),
		)
	}

	w.WriteHeader(http.StatusNoContent)
}
