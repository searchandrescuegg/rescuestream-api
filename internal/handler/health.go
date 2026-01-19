package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthHandler handles health check requests.
type HealthHandler struct {
	pool *pgxpool.Pool
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(pool *pgxpool.Pool) *HealthHandler {
	return &HealthHandler{pool: pool}
}

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database,omitempty"`
}

// ServeHTTP handles GET /health requests.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, r, ErrInvalidRequest("method not allowed"))
		return
	}

	resp := HealthResponse{
		Status: "ok",
	}

	// Check database connectivity
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.pool.Ping(ctx); err != nil {
		resp.Status = "degraded"
		resp.Database = "unreachable"
	} else {
		resp.Database = "ok"
	}

	if resp.Status != "ok" {
		WriteJSON(w, http.StatusServiceUnavailable, resp)
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}
