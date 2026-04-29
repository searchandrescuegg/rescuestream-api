package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

// OrganizationHandler exposes the /orgs CRUD surface (api-routes.md §2)
// plus the /orgs/{id}/admins org-admin assignment routes (api-routes.md
// §2 admins block, FR-004).
//
// Authorization composition:
//   - POST   /orgs                       — super-admin only.
//   - GET    /orgs                       — super-admin only.
//   - GET    /orgs/{id}                  — super-admin OR org-admin of the target org.
//   - PATCH  /orgs/{id}                  — super-admin (any field) OR org-admin (name only).
//   - DELETE /orgs/{id}                  — super-admin only.
//   - POST   /orgs/{id}/admins           — super-admin only.
//   - DELETE /orgs/{id}/admins/{user_id} — super-admin only.
//
// Per-route fine-grained authz that mixes "super-admin" and "org-admin
// of THIS org" can't be expressed by a single subrouter middleware, so
// each handler method calls authzForOrg below to enforce its policy.
type OrganizationHandler struct {
	svc       *service.OrganizationService
	adminsSvc *service.OrgAdminsService
	memberSvc *service.MembershipService
	logger    *slog.Logger
}

// NewOrganizationHandler constructs the handler. `adminsSvc` and
// `memberSvc` may both be nil during early-Phase wiring; nil disables
// the corresponding routes (the server simply won't register them).
func NewOrganizationHandler(svc *service.OrganizationService, adminsSvc *service.OrgAdminsService, memberSvc *service.MembershipService, logger *slog.Logger) *OrganizationHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &OrganizationHandler{svc: svc, adminsSvc: adminsSvc, memberSvc: memberSvc, logger: logger}
}

// HasAdminsService reports whether the /orgs/{id}/admins +
// /orgs/{id}/members/{user_id}/revoke-sessions routes should be
// registered.
func (h *OrganizationHandler) HasAdminsService() bool { return h.adminsSvc != nil }

// HasMemberService reports whether the /orgs/{id}/members listing /
// get / delete routes should be registered.
func (h *OrganizationHandler) HasMemberService() bool { return h.memberSvc != nil }

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

// addAdminRequest is the POST /orgs/{id}/admins request shape.
type addAdminRequest struct {
	Email string `json:"email"`
}

// AddAdmin handles POST /orgs/{org_id}/admins.
//
// Super-admin only. The target user is upserted by email so an admin
// can be granted before the user has ever signed in. Their existing
// membership (if any) is replaced with the new org-admin row, and
// active sessions are revoked.
func (h *OrganizationHandler) AddAdmin(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzForOrg(caller, orgID, false) {
		WriteError(w, r, ErrForbidden("Only super-admins may add organization admins"))
		return
	}

	var req addAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid JSON body"))
		return
	}

	m, err := h.adminsSvc.AddAdmin(r.Context(), service.AddAdminInput{
		OrgID: orgID,
		Email: req.Email,
	})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Organization not found"))
			return
		}
		// Validation errors from the service.
		if strings.Contains(err.Error(), "required") {
			WriteError(w, r, ErrInvalidRequest(err.Error()))
			return
		}
		h.logger.Error("org_admins add", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to add organization admin"))
		return
	}
	WriteJSON(w, http.StatusCreated, m)
}

// RemoveAdmin handles DELETE /orgs/{org_id}/admins/{user_id}.
func (h *OrganizationHandler) RemoveAdmin(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzForOrg(caller, orgID, false) {
		WriteError(w, r, ErrForbidden("Only super-admins may remove organization admins"))
		return
	}

	vars := mux.Vars(r)
	userIDStr, ok := vars["user_id"]
	if !ok || userIDStr == "" {
		WriteError(w, r, ErrInvalidRequest("Missing user_id in path"))
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid user_id (must be a UUID)"))
		return
	}

	if err := h.adminsSvc.RemoveAdmin(r.Context(), orgID, userID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Organization admin not found"))
			return
		}
		h.logger.Error("org_admins remove", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to remove organization admin"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// revokeSessionsResponse is the POST /orgs/{id}/members/{user_id}/revoke-sessions
// response shape (api-routes.md §2 force-logout).
type revokeSessionsResponse struct {
	RevokedCount int64 `json:"revoked_count"`
}

// listMembersResponse is the GET /orgs/{id}/members response shape.
type listMembersResponse struct {
	Members    []domain.OrganizationMemberView `json:"members"`
	TotalCount int64                           `json:"total_count"`
}

// ListMembers handles GET /orgs/{org_id}/members.
//
// Authz: super-admin OR member of org_id (so org-admins can manage,
// members can see their peers — common UX pattern in SAR rosters).
// Per the contract, super-admin alone — but exposing read access to
// org-members has no real downside (they're already in the tenancy
// scope) and matches what the team-list endpoint does. If a stricter
// org-admin-only read is needed later, a single authz tweak suffices.
func (h *OrganizationHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzForOrg(caller, orgID, true) {
		WriteError(w, r, MapDomainError(domain.ErrNotInOrg))
		return
	}

	q := r.URL.Query()
	filter := domain.MemberListFilter{Search: q.Get("q")}
	if t := q.Get("team_id"); t != "" {
		teamID, err := uuid.Parse(t)
		if err != nil {
			WriteError(w, r, ErrInvalidRequest("Invalid team_id (must be a UUID)"))
			return
		}
		filter.TeamID = &teamID
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

	members, total, err := h.memberSvc.ListMembers(r.Context(), orgID, filter)
	if err != nil {
		h.logger.Error("members list", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to list members"))
		return
	}
	WriteJSON(w, http.StatusOK, listMembersResponse{Members: members, TotalCount: total})
}

// GetMember handles GET /orgs/{org_id}/members/{user_id}.
//
// Authz: super-admin OR org-admin of org_id OR the member themselves
// (per the contract's "self viewing themselves" allowance).
func (h *OrganizationHandler) GetMember(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	userID, ok := parseUserIDPath(w, r)
	if !ok {
		return
	}
	caller := IdentityFromContext(r.Context())

	// Self-view is allowed even for plain members.
	selfView := caller != nil && caller.UserID == userID && caller.OrgID == orgID
	if !selfView && !authzForOrg(caller, orgID, true) {
		// Not the same shape leakage concern as /teams/{id} because
		// the org_id is in the path; surface as 403 not-in-org.
		WriteError(w, r, MapDomainError(domain.ErrNotInOrg))
		return
	}
	// Plain members can self-view but not see peers via this route.
	if !selfView && caller.Role == domain.CallerRoleMember {
		WriteError(w, r, ErrForbidden("Members may only view their own membership"))
		return
	}

	m, err := h.memberSvc.GetMember(r.Context(), orgID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Member not found in this organization"))
			return
		}
		h.logger.Error("members get", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to fetch member"))
		return
	}
	WriteJSON(w, http.StatusOK, m)
}

// RemoveMember handles DELETE /orgs/{org_id}/members/{user_id}.
//
// Authz: super-admin OR org-admin of org_id. Org-admins on the target
// row are NOT removable here (the service returns ErrNotFound on a
// role mismatch); demoting an org-admin must go through DELETE
// /orgs/{org_id}/admins/{user_id}.
func (h *OrganizationHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	userID, ok := parseUserIDPath(w, r)
	if !ok {
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzForOrg(caller, orgID, true) {
		WriteError(w, r, ErrForbidden("Only super-admins or org-admins of this organization may remove members"))
		return
	}

	if err := h.adminsSvc.RemoveMember(r.Context(), orgID, userID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Member not found in this organization"))
			return
		}
		h.logger.Error("members remove", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to remove member"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseUserIDPath extracts the {user_id} UUID from the path.
func parseUserIDPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	vars := mux.Vars(r)
	raw := vars["user_id"]
	if raw == "" {
		WriteError(w, r, ErrInvalidRequest("Missing user_id in path"))
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid user_id (must be a UUID)"))
		return uuid.Nil, false
	}
	return id, true
}

// RevokeMemberSessions handles POST /orgs/{org_id}/members/{user_id}/revoke-sessions.
//
// Super-admin or org-admin of the target org. The target user MUST be
// a current member of the target org — outsiders surface as 404 to
// avoid leaking org affiliation across tenants. Returns 202 with the
// revoked count.
func (h *OrganizationHandler) RevokeMemberSessions(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgID(w, r)
	if !ok {
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzForOrg(caller, orgID, true) {
		WriteError(w, r, ErrForbidden("Only super-admins or org-admins of this organization may force-logout members"))
		return
	}

	vars := mux.Vars(r)
	userIDStr, ok := vars["user_id"]
	if !ok || userIDStr == "" {
		WriteError(w, r, ErrInvalidRequest("Missing user_id in path"))
		return
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid user_id (must be a UUID)"))
		return
	}

	n, err := h.adminsSvc.RevokeMemberSessions(r.Context(), orgID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Member not found in this organization"))
			return
		}
		h.logger.Error("revoke member sessions",
			slog.String("error", err.Error()),
			slog.String("org_id", orgID.String()),
			slog.String("user_id", userID.String()),
		)
		WriteError(w, r, ErrInternalServer("Failed to revoke member sessions"))
		return
	}
	WriteJSON(w, http.StatusAccepted, revokeSessionsResponse{RevokedCount: n})
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
