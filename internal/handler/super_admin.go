package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

// SuperAdminHandler exposes the GET / POST / DELETE /super-admins
// endpoints (api-routes.md §3, FR-005). All routes require super-admin
// authorization — wire them behind RequireSuperAdmin in the router.
type SuperAdminHandler struct {
	svc    *service.SuperAdminService
	logger *slog.Logger
}

// NewSuperAdminHandler constructs the handler.
func NewSuperAdminHandler(svc *service.SuperAdminService, logger *slog.Logger) *SuperAdminHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SuperAdminHandler{svc: svc, logger: logger}
}

// listResponse is the GET /super-admins response shape.
type listSuperAdminsResponse struct {
	SuperAdmins []service.SuperAdminEntry `json:"super_admins"`
}

// addRequest is the POST /super-admins request shape.
type addSuperAdminRequest struct {
	Email string `json:"email"`
}

// List handles GET /super-admins.
func (h *SuperAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.List(r.Context())
	if err != nil {
		h.logger.Error("super_admin list", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to list super-admins"))
		return
	}
	WriteJSON(w, http.StatusOK, listSuperAdminsResponse{SuperAdmins: rows})
}

// Add handles POST /super-admins.
func (h *SuperAdminHandler) Add(w http.ResponseWriter, r *http.Request) {
	var req addSuperAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid JSON body"))
		return
	}
	if req.Email == "" {
		WriteError(w, r, ErrInvalidRequest("`email` is required"))
		return
	}

	caller := IdentityFromContext(r.Context())
	var grantedBy *uuid.UUID
	if caller != nil {
		gb := caller.UserID
		grantedBy = &gb
	}

	entry, err := h.svc.AddByEmail(r.Context(), service.AddByEmailInput{
		Email:     req.Email,
		GrantedBy: grantedBy,
	})
	if err != nil {
		h.logger.Error("super_admin add", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to add super-admin"))
		return
	}
	WriteJSON(w, http.StatusCreated, entry)
}

// Remove handles DELETE /super-admins/{user_id}.
func (h *SuperAdminHandler) Remove(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userIDStr, ok := vars["user_id"]
	if !ok {
		WriteError(w, r, ErrInvalidRequest("Missing user_id in path"))
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid user_id (must be a UUID)"))
		return
	}

	if err := h.svc.Remove(r.Context(), userID); err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound):
			WriteError(w, r, ErrNotFound("Super-admin not found"))
			return
		case errors.Is(err, domain.ErrLastSuperAdmin):
			WriteError(w, r, MapDomainError(domain.ErrLastSuperAdmin))
			return
		default:
			h.logger.Error("super_admin remove", slog.String("error", err.Error()))
			WriteError(w, r, ErrInternalServer("Failed to remove super-admin"))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
