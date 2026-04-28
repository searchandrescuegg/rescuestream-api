package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

// OrganizationHandler exposes the /orgs CRUD surface (api-routes.md §2).
//
// Authorization composition:
//   - POST   /orgs            — super-admin only (route wrapped with RequireSuperAdmin).
//   - GET    /orgs            — super-admin only.
//   - GET    /orgs/{id}       — super-admin OR org-admin of the target org.
//   - PATCH  /orgs/{id}       — super-admin (any field) OR org-admin (name only).
//   - DELETE /orgs/{id}       — super-admin only.
//
// Per-route fine-grained authz that mixes "super-admin" and "org-admin
// of THIS org" can't be expressed by a single subrouter middleware, so
// each handler method calls authzForOrg below to enforce its policy.
type OrganizationHandler struct {
	svc    *service.OrganizationService
	logger *slog.Logger
}

// NewOrganizationHandler constructs the handler.
func NewOrganizationHandler(svc *service.OrganizationService, logger *slog.Logger) *OrganizationHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &OrganizationHandler{svc: svc, logger: logger}
}

// authzForOrg returns true when the caller is allowed to access the
// given target org under the policy described. `orgAdminOK` is true when
// org-admins of this specific org should be admitted in addition to
// super-admins.
func authzForOrg(ident *domain.CallerIdentity, targetOrg uuid.UUID, orgAdminOK bool) bool {
	if ident == nil {
		return false
	}
	if ident.IsSuperAdmin() {
		return true
	}
	if !orgAdminOK {
		return false
	}
	return ident.Role == domain.CallerRoleOrgAdmin && ident.OrgID == targetOrg
}

// --- Wire shapes -----------------------------------------------------

type createOrgRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
	// InitialAdminEmails is accepted by the contract for forward
	// compatibility; the orchestrated upsert (user → org-admin
	// membership → session revocation) lands with the org-admins
	// service in a subsequent commit, at which point this handler will
	// loop over the list. Currently emitted as a TODO if non-empty.
	InitialAdminEmails []string `json:"initial_admin_emails,omitempty"`
}

type updateOrgRequest struct {
	Name   *string                    `json:"name,omitempty"`
	Status *domain.OrganizationStatus `json:"status,omitempty"`
}

type listOrgsResponse struct {
	Orgs       []domain.Organization `json:"orgs"`
	TotalCount int64                 `json:"total_count"`
}

// --- Handlers --------------------------------------------------------

// Create handles POST /orgs.
func (h *OrganizationHandler) Create(w http.ResponseWriter, r *http.Request) {
	caller := IdentityFromContext(r.Context())
	if !authzForOrg(caller, uuid.Nil, false) {
		WriteError(w, r, ErrForbidden("Only super-admins may create organizations"))
		return
	}

	var req createOrgRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid JSON body"))
		return
	}

	org, err := h.svc.Create(r.Context(), service.CreateOrgInput{
		Name:            req.Name,
		Slug:            req.Slug,
		CreatedByUserID: caller.UserID,
	})
	if err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			WriteError(w, r, ErrConflict("An organization with that slug already exists"))
			return
		}
		// Validation errors come back as plain wrapped errors with
		// descriptive messages — surface as 400.
		WriteError(w, r, ErrInvalidRequest(err.Error()))
		return
	}

	if len(req.InitialAdminEmails) > 0 {
		// Initial-admin orchestration is out of scope for this commit;
		// the contract permits the field for forward compat. Log so
		// callers can see the field was accepted-but-deferred.
		h.logger.Warn("initial_admin_emails ignored — admin orchestration lands in a follow-up commit",
			slog.Int("emails_count", len(req.InitialAdminEmails)),
			slog.String("org_id", org.ID.String()),
		)
	}

	WriteJSON(w, http.StatusCreated, org)
}

// List handles GET /orgs.
func (h *OrganizationHandler) List(w http.ResponseWriter, r *http.Request) {
	caller := IdentityFromContext(r.Context())
	if !authzForOrg(caller, uuid.Nil, false) {
		WriteError(w, r, ErrForbidden("Only super-admins may list organizations"))
		return
	}

	q := r.URL.Query()
	filter := domain.OrganizationListFilter{
		Search: q.Get("q"),
		Status: domain.OrganizationStatus(q.Get("status")),
	}
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			filter.Limit = n
		}
	}
	if o := q.Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			filter.Offset = n
		}
	}

	orgs, total, err := h.svc.List(r.Context(), filter)
	if err != nil {
		h.logger.Error("organization list", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to list organizations"))
		return
	}
	WriteJSON(w, http.StatusOK, listOrgsResponse{Orgs: orgs, TotalCount: total})
}

// Get handles GET /orgs/{id}.
func (h *OrganizationHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzForOrg(caller, id, true) {
		WriteError(w, r, MapDomainError(domain.ErrNotInOrg))
		return
	}

	org, err := h.svc.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Organization not found"))
			return
		}
		h.logger.Error("organization get", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to fetch organization"))
		return
	}
	WriteJSON(w, http.StatusOK, org)
}

// Update handles PATCH /orgs/{id}.
func (h *OrganizationHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzForOrg(caller, id, true) {
		WriteError(w, r, MapDomainError(domain.ErrNotInOrg))
		return
	}

	var req updateOrgRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid JSON body"))
		return
	}

	// Org-admins may only rename. Status changes are super-admin only.
	if req.Status != nil && !caller.IsSuperAdmin() {
		WriteError(w, r, ErrForbidden("Only super-admins may change organization status"))
		return
	}

	org, err := h.svc.Update(r.Context(), id, service.UpdateOrgInput{
		Name:   req.Name,
		Status: req.Status,
	})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Organization not found"))
			return
		}
		WriteError(w, r, ErrInvalidRequest(err.Error()))
		return
	}
	WriteJSON(w, http.StatusOK, org)
}

// Delete handles DELETE /orgs/{id}.
func (h *OrganizationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzForOrg(caller, id, false) {
		WriteError(w, r, ErrForbidden("Only super-admins may delete organizations"))
		return
	}

	if err := h.svc.Delete(r.Context(), id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Organization not found"))
			return
		}
		h.logger.Error("organization delete", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to delete organization"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseOrgID extracts the UUID at {id} or {org_id}; returns (uuid.Nil,
// false) and writes a 400 on bad input.
func parseOrgID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	vars := mux.Vars(r)
	raw := vars["id"]
	if raw == "" {
		raw = vars["org_id"]
	}
	if raw == "" {
		WriteError(w, r, ErrInvalidRequest("Missing organization id in path"))
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid organization id (must be a UUID)"))
		return uuid.Nil, false
	}
	return id, true
}
