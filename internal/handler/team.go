package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

// TeamHandler exposes the team CRUD surface (api-routes.md §4):
//
//   - POST   /orgs/{org_id}/teams        — org-admin of org_id (or super-admin)
//   - GET    /orgs/{org_id}/teams        — member of org_id (or super-admin)
//   - GET    /teams/{team_id}            — member of the team's org (or super-admin)
//   - PATCH  /teams/{team_id}            — org-admin of the team's org (or super-admin)
//   - DELETE /teams/{team_id}            — org-admin of the team's org (or super-admin)
//
// "Member of org_id" includes org-admins. The handler internally
// resolves the team's org_id when the route uses /teams/{id} to gate
// authz against the resolved org rather than the path parameter.
type TeamHandler struct {
	svc    *service.TeamService
	logger *slog.Logger
}

// NewTeamHandler constructs a TeamHandler.
func NewTeamHandler(svc *service.TeamService, logger *slog.Logger) *TeamHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TeamHandler{svc: svc, logger: logger}
}

// --- Wire shapes -----------------------------------------------------

type createTeamRequest struct {
	Name            string `json:"name"`
	WorkspaceDomain string `json:"workspace_domain"`
}

type updateTeamRequest struct {
	Name            *string `json:"name,omitempty"`
	WorkspaceDomain *string `json:"workspace_domain,omitempty"`
}

type listTeamsResponse struct {
	Teams []domain.Team `json:"teams"`
}

// --- Authz helpers ---------------------------------------------------

// authzMemberOfOrg returns true when the caller is super-admin or any
// member (incl. org-admin) of orgID. Used for read endpoints under
// /orgs/{org_id}/teams.
func authzMemberOfOrg(ident *domain.CallerIdentity, orgID uuid.UUID) bool {
	if ident == nil {
		return false
	}
	if ident.IsSuperAdmin() {
		return true
	}
	return ident.OrgID == orgID
}

// authzOrgAdminOfOrg returns true when the caller is super-admin or
// org-admin of orgID. Used for write endpoints (Create/Patch/Delete).
func authzOrgAdminOfOrg(ident *domain.CallerIdentity, orgID uuid.UUID) bool {
	if ident == nil {
		return false
	}
	if ident.IsSuperAdmin() {
		return true
	}
	return ident.Role == domain.CallerRoleOrgAdmin && ident.OrgID == orgID
}

// --- Handlers --------------------------------------------------------

// Create handles POST /orgs/{org_id}/teams.
func (h *TeamHandler) Create(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgIDFromVar(w, r, "org_id")
	if !ok {
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzOrgAdminOfOrg(caller, orgID) {
		WriteError(w, r, ErrForbidden("Only org-admins of this organization may create teams"))
		return
	}

	var req createTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid JSON body"))
		return
	}

	team, err := h.svc.Create(r.Context(), service.CreateTeamInput{
		OrganizationID:  orgID,
		Name:            req.Name,
		WorkspaceDomain: req.WorkspaceDomain,
	})
	if err != nil {
		if errors.Is(err, domain.ErrWorkspaceDomainTaken) {
			WriteError(w, r, MapDomainError(domain.ErrWorkspaceDomainTaken))
			return
		}
		// Validation errors come back as plain wrapped errors with
		// descriptive messages — surface as 400.
		if strings.Contains(err.Error(), "team:") {
			WriteError(w, r, ErrInvalidRequest(err.Error()))
			return
		}
		h.logger.Error("team create", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to create team"))
		return
	}
	WriteJSON(w, http.StatusCreated, team)
}

// ListByOrg handles GET /orgs/{org_id}/teams.
func (h *TeamHandler) ListByOrg(w http.ResponseWriter, r *http.Request) {
	orgID, ok := parseOrgIDFromVar(w, r, "org_id")
	if !ok {
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzMemberOfOrg(caller, orgID) {
		WriteError(w, r, MapDomainError(domain.ErrNotInOrg))
		return
	}

	teams, err := h.svc.ListByOrg(r.Context(), orgID)
	if err != nil {
		h.logger.Error("team list", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to list teams"))
		return
	}
	WriteJSON(w, http.StatusOK, listTeamsResponse{Teams: teams})
}

// Get handles GET /teams/{team_id}. The team's org is resolved before
// the authz check so the response doesn't leak whether a team_id is
// valid to callers who can't see it.
func (h *TeamHandler) Get(w http.ResponseWriter, r *http.Request) {
	teamID, ok := parseTeamID(w, r)
	if !ok {
		return
	}
	team, err := h.svc.Get(r.Context(), teamID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Team not found"))
			return
		}
		h.logger.Error("team get", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to fetch team"))
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzMemberOfOrg(caller, team.OrganizationID) {
		// Surface as 404 rather than 403 to avoid leaking the team's
		// existence to callers in a different org.
		WriteError(w, r, ErrNotFound("Team not found"))
		return
	}
	WriteJSON(w, http.StatusOK, team)
}

// Update handles PATCH /teams/{team_id}.
func (h *TeamHandler) Update(w http.ResponseWriter, r *http.Request) {
	teamID, ok := parseTeamID(w, r)
	if !ok {
		return
	}
	team, err := h.svc.Get(r.Context(), teamID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Team not found"))
			return
		}
		h.logger.Error("team update get", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to fetch team"))
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzOrgAdminOfOrg(caller, team.OrganizationID) {
		WriteError(w, r, ErrNotFound("Team not found"))
		return
	}

	var req updateTeamRequest
	if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid JSON body"))
		return
	}
	updated, err := h.svc.Update(r.Context(), teamID, service.UpdateTeamInput{
		Name:            req.Name,
		WorkspaceDomain: req.WorkspaceDomain,
	})
	if err != nil {
		if errors.Is(err, domain.ErrWorkspaceDomainTaken) {
			WriteError(w, r, MapDomainError(domain.ErrWorkspaceDomainTaken))
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Team not found"))
			return
		}
		if strings.Contains(err.Error(), "team:") {
			WriteError(w, r, ErrInvalidRequest(err.Error()))
			return
		}
		h.logger.Error("team update", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to update team"))
		return
	}
	WriteJSON(w, http.StatusOK, updated)
}

// Delete handles DELETE /teams/{team_id}.
//
// The full coordinated-deletion transaction (member removal + session
// revocation + org-admin team_id NULL-out per data-model §1.4) is owed
// by the next commit. Until then, attempting to delete a team that
// still has dependent member memberships will fail with a FK violation
// from the RESTRICT constraint — that's the desired loud failure (per
// the data-model invariants comment) rather than silent corruption.
func (h *TeamHandler) Delete(w http.ResponseWriter, r *http.Request) {
	teamID, ok := parseTeamID(w, r)
	if !ok {
		return
	}
	team, err := h.svc.Get(r.Context(), teamID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Team not found"))
			return
		}
		h.logger.Error("team delete get", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to fetch team"))
		return
	}
	caller := IdentityFromContext(r.Context())
	if !authzOrgAdminOfOrg(caller, team.OrganizationID) {
		WriteError(w, r, ErrNotFound("Team not found"))
		return
	}

	if err := h.svc.Delete(r.Context(), teamID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			WriteError(w, r, ErrNotFound("Team not found"))
			return
		}
		h.logger.Error("team delete", slog.String("error", err.Error()))
		WriteError(w, r, ErrInternalServer("Failed to delete team (the coordinated member-removal transaction is owed by a follow-up commit)"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseOrgIDFromVar extracts the UUID at the given path-var name.
func parseOrgIDFromVar(w http.ResponseWriter, r *http.Request, varName string) (uuid.UUID, bool) {
	vars := mux.Vars(r)
	raw := vars[varName]
	if raw == "" {
		WriteError(w, r, ErrInvalidRequest("Missing "+varName+" in path"))
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid "+varName+" (must be a UUID)"))
		return uuid.Nil, false
	}
	return id, true
}

// parseTeamID extracts the UUID at {team_id}.
func parseTeamID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	return parseOrgIDFromVar(w, r, "team_id")
}
