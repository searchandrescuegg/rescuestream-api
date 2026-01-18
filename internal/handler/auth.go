package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

// AuthHandler handles MediaMTX authentication requests.
type AuthHandler struct {
	authService *service.AuthService
	logger      *slog.Logger
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(authService *service.AuthService, logger *slog.Logger) *AuthHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthHandler{
		authService: authService,
		logger:      logger,
	}
}

// ServeHTTP handles POST /auth requests from MediaMTX.
func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, r, ErrInvalidRequest("method not allowed"))
		return
	}

	var req service.AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("failed to decode auth request",
			slog.String("error", err.Error()),
			slog.String("remote_addr", r.RemoteAddr),
		)
		WriteError(w, r, ErrInvalidRequest("invalid request body"))
		return
	}

	result, err := h.authService.Authenticate(r.Context(), req)
	if err != nil {
		h.logger.Error("authentication error",
			slog.String("error", err.Error()),
			slog.String("path", req.Path),
			slog.String("ip", req.IP),
		)
		WriteError(w, r, ErrInternalServer("authentication failed"))
		return
	}

	if !result.Allowed {
		h.logger.Info("authentication rejected",
			slog.String("reason", result.Reason),
			slog.String("path", req.Path),
			slog.String("ip", req.IP),
			slog.String("action", req.Action),
		)
		// MediaMTX expects 401 for rejection
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// MediaMTX expects 200 for success
	w.WriteHeader(http.StatusOK)
}
