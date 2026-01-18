package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

// BroadcasterHandler handles broadcaster management HTTP requests.
type BroadcasterHandler struct {
	broadcasterService *service.BroadcasterService
	logger             *slog.Logger
}

// NewBroadcasterHandler creates a new BroadcasterHandler.
func NewBroadcasterHandler(broadcasterService *service.BroadcasterService, logger *slog.Logger) *BroadcasterHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &BroadcasterHandler{
		broadcasterService: broadcasterService,
		logger:             logger,
	}
}

// CreateBroadcasterRequest represents the request body for creating a broadcaster.
type CreateBroadcasterRequest struct {
	DisplayName string                 `json:"display_name"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateBroadcasterRequest represents the request body for updating a broadcaster.
type UpdateBroadcasterRequest struct {
	DisplayName *string                `json:"display_name,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// BroadcasterListResponse represents the response for listing broadcasters.
type BroadcasterListResponse struct {
	Broadcasters []domain.Broadcaster `json:"broadcasters"`
	Count        int                  `json:"count"`
}

// ServeHTTP routes broadcaster requests to the appropriate handler.
func (h *BroadcasterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	switch {
	case r.Method == http.MethodGet && id == "":
		h.listBroadcasters(w, r)
	case r.Method == http.MethodPost && id == "":
		h.createBroadcaster(w, r)
	case r.Method == http.MethodGet && id != "":
		h.getBroadcaster(w, r, id)
	case r.Method == http.MethodPatch && id != "":
		h.updateBroadcaster(w, r, id)
	case r.Method == http.MethodDelete && id != "":
		h.deleteBroadcaster(w, r, id)
	default:
		WriteError(w, r, ErrInvalidRequest("method not allowed"))
	}
}

func (h *BroadcasterHandler) listBroadcasters(w http.ResponseWriter, r *http.Request) {
	broadcasters, err := h.broadcasterService.List(r.Context())
	if err != nil {
		h.logger.Error("failed to list broadcasters", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("failed to list broadcasters"))
		return
	}

	if broadcasters == nil {
		broadcasters = []domain.Broadcaster{}
	}

	resp := BroadcasterListResponse{
		Broadcasters: broadcasters,
		Count:        len(broadcasters),
	}

	WriteJSON(w, http.StatusOK, resp)
}

func (h *BroadcasterHandler) createBroadcaster(w http.ResponseWriter, r *http.Request) {
	var req CreateBroadcasterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, ErrInvalidRequest("invalid request body"))
		return
	}

	if req.DisplayName == "" {
		WriteError(w, r, ErrInvalidRequest("display_name is required"))
		return
	}

	broadcaster, err := h.broadcasterService.Create(r.Context(), service.CreateBroadcasterRequest{
		DisplayName: req.DisplayName,
		Metadata:    req.Metadata,
	})
	if err != nil {
		httpErr := MapDomainError(err)
		WriteError(w, r, httpErr)
		return
	}

	WriteJSON(w, http.StatusCreated, broadcaster)
}

func (h *BroadcasterHandler) getBroadcaster(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("invalid broadcaster ID"))
		return
	}

	broadcaster, err := h.broadcasterService.GetByID(r.Context(), id)
	if err != nil {
		httpErr := MapDomainError(err)
		WriteError(w, r, httpErr)
		return
	}

	WriteJSON(w, http.StatusOK, broadcaster)
}

func (h *BroadcasterHandler) updateBroadcaster(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("invalid broadcaster ID"))
		return
	}

	var req UpdateBroadcasterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, ErrInvalidRequest("invalid request body"))
		return
	}

	broadcaster, err := h.broadcasterService.Update(r.Context(), id, service.UpdateBroadcasterRequest{
		DisplayName: req.DisplayName,
		Metadata:    req.Metadata,
	})
	if err != nil {
		httpErr := MapDomainError(err)
		WriteError(w, r, httpErr)
		return
	}

	WriteJSON(w, http.StatusOK, broadcaster)
}

func (h *BroadcasterHandler) deleteBroadcaster(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("invalid broadcaster ID"))
		return
	}

	if err := h.broadcasterService.Delete(r.Context(), id); err != nil {
		httpErr := MapDomainError(err)
		WriteError(w, r, httpErr)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
