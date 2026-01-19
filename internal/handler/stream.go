package handler

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

// StreamHandler handles stream-related HTTP requests.
type StreamHandler struct {
	streamService *service.StreamService
	logger        *slog.Logger
}

// NewStreamHandler creates a new StreamHandler.
func NewStreamHandler(streamService *service.StreamService, logger *slog.Logger) *StreamHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &StreamHandler{
		streamService: streamService,
		logger:        logger,
	}
}

// StreamListResponse represents the response for listing streams.
type StreamListResponse struct {
	Streams []domain.StreamWithURLs `json:"streams"`
	Count   int                     `json:"count"`
}

// ServeHTTP routes stream requests to the appropriate handler.
func (h *StreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	switch {
	case r.Method == http.MethodGet && id == "":
		h.listStreams(w, r)
	case r.Method == http.MethodGet && id != "":
		h.getStream(w, r, id)
	default:
		WriteError(w, r, ErrInvalidRequest("method not allowed"))
	}
}

func (h *StreamHandler) listStreams(w http.ResponseWriter, r *http.Request) {
	streams, err := h.streamService.ListActive(r.Context())
	if err != nil {
		h.logger.Error("failed to list streams", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("failed to list streams"))
		return
	}

	if streams == nil {
		streams = []domain.StreamWithURLs{}
	}

	resp := StreamListResponse{
		Streams: streams,
		Count:   len(streams),
	}

	WriteJSON(w, http.StatusOK, resp)
}

func (h *StreamHandler) getStream(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := uuid.Parse(idStr)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("invalid stream ID"))
		return
	}

	stream, err := h.streamService.GetByID(r.Context(), id)
	if err != nil {
		httpErr := MapDomainError(err)
		WriteError(w, r, httpErr)
		return
	}

	WriteJSON(w, http.StatusOK, stream)
}
