package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

// StreamKeyHandler handles stream key management HTTP requests.
type StreamKeyHandler struct {
	streamKeyService *service.StreamKeyService
	logger           *slog.Logger
}

// NewStreamKeyHandler creates a new StreamKeyHandler.
func NewStreamKeyHandler(streamKeyService *service.StreamKeyService, logger *slog.Logger) *StreamKeyHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &StreamKeyHandler{
		streamKeyService: streamKeyService,
		logger:           logger,
	}
}

// CreateStreamKeyRequest represents the request body for creating a stream key.
type CreateStreamKeyRequest struct {
	BroadcasterID string  `json:"broadcaster_id"`
	ExpiresAt     *string `json:"expires_at,omitempty"`
}

// StreamKeyListResponse represents the response for listing stream keys.
type StreamKeyListResponse struct {
	StreamKeys []domain.StreamKey `json:"stream_keys"`
	Count      int                `json:"count"`
}

// ServeHTTP routes stream key requests to the appropriate handler.
func (h *StreamKeyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	switch {
	case r.Method == http.MethodGet && id == "":
		h.listStreamKeys(w, r)
	case r.Method == http.MethodPost && id == "":
		h.createStreamKey(w, r)
	case r.Method == http.MethodGet && id != "":
		h.getStreamKey(w, r, id)
	case r.Method == http.MethodDelete && id != "":
		h.revokeStreamKey(w, r, id)
	default:
		WriteError(w, r, ErrInvalidRequest("method not allowed"))
	}
}

func (h *StreamKeyHandler) listStreamKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.streamKeyService.List(r.Context())
	if err != nil {
		h.logger.Error("failed to list stream keys", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("failed to list stream keys"))
		return
	}

	if keys == nil {
		keys = []domain.StreamKey{}
	}

	resp := StreamKeyListResponse{
		StreamKeys: keys,
		Count:      len(keys),
	}

	WriteJSON(w, http.StatusOK, resp)
}

func (h *StreamKeyHandler) createStreamKey(w http.ResponseWriter, r *http.Request) {
	var req CreateStreamKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, ErrInvalidRequest("invalid request body"))
		return
	}

	if req.BroadcasterID == "" {
		WriteError(w, r, ErrInvalidRequest("broadcaster_id is required"))
		return
	}

	broadcasterID, err := uuid.Parse(req.BroadcasterID)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("invalid broadcaster_id"))
		return
	}

	createReq := service.CreateRequest{
		BroadcasterID: broadcasterID,
	}

	if req.ExpiresAt != nil {
		expiresAt, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			WriteError(w, r, ErrInvalidRequest("invalid expires_at format, use RFC3339"))
			return
		}
		createReq.ExpiresAt = &expiresAt
	}

	key, err := h.streamKeyService.Create(r.Context(), createReq)
	if err != nil {
		httpErr := MapDomainError(err)
		WriteError(w, r, httpErr)
		return
	}

	WriteJSON(w, http.StatusCreated, key)
}

func (h *StreamKeyHandler) getStreamKey(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("invalid stream key ID"))
		return
	}

	key, err := h.streamKeyService.GetByID(r.Context(), id)
	if err != nil {
		httpErr := MapDomainError(err)
		WriteError(w, r, httpErr)
		return
	}

	// Clear key value for security
	key.KeyValue = ""

	WriteJSON(w, http.StatusOK, key)
}

func (h *StreamKeyHandler) revokeStreamKey(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("invalid stream key ID"))
		return
	}

	if err := h.streamKeyService.Revoke(r.Context(), id); err != nil {
		httpErr := MapDomainError(err)
		WriteError(w, r, httpErr)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
