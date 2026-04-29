package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// TeamService is the business-logic boundary for team CRUD. Wraps
// TeamRepository with workspace-domain normalization + validation and
// the lifecycle invariants FR-006 / FR-007 mandate.
//
// Team deletion's coordinated transaction (member removal + session
// revocation + org-admin team_id NULL-out per spec edge case) lives
// here so handlers stay HTTP-shaped. The MembershipRepository,
// SuperAdminRepository, and SessionService dependencies wire it up.
type TeamService struct {
	teams       domain.TeamRepository
	memberships domain.MembershipRepository
	sessions    *SessionService
}

// NewTeamService constructs a TeamService.
func NewTeamService(
	teams domain.TeamRepository,
	memberships domain.MembershipRepository,
	sessions *SessionService,
) *TeamService {
	return &TeamService{teams: teams, memberships: memberships, sessions: sessions}
}

// CreateTeamInput is the input for Create.
type CreateTeamInput struct {
	OrganizationID  uuid.UUID
	Name            string
	WorkspaceDomain string
}

// Create validates inputs and inserts a new team.
func (s *TeamService) Create(ctx context.Context, in CreateTeamInput) (*domain.Team, error) {
	name := strings.TrimSpace(in.Name)
	dom := domain.NormalizeWorkspaceDomain(in.WorkspaceDomain)

	if in.OrganizationID == uuid.Nil {
		return nil, fmt.Errorf("team: organization_id required")
	}
	if name == "" {
		return nil, fmt.Errorf("team: name is required")
	}
	if err := domain.ValidateWorkspaceDomain(dom); err != nil {
		return nil, fmt.Errorf("team: invalid workspace_domain (must be a lowercase dotted hostname, ≤253 chars)")
	}
	return s.teams.Create(ctx, domain.TeamCreate{
		OrganizationID:  in.OrganizationID,
		Name:            name,
		WorkspaceDomain: dom,
	})
}

// Get fetches a team by id.
func (s *TeamService) Get(ctx context.Context, id uuid.UUID) (*domain.Team, error) {
	return s.teams.FindByID(ctx, id)
}

// ListByOrg returns every team in the org.
func (s *TeamService) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]domain.Team, error) {
	return s.teams.ListByOrg(ctx, orgID)
}

// UpdateTeamInput is the input for Update.
type UpdateTeamInput struct {
	Name            *string
	WorkspaceDomain *string
}

// Update applies an optional rename and/or workspace-domain change.
func (s *TeamService) Update(ctx context.Context, id uuid.UUID, in UpdateTeamInput) (*domain.Team, error) {
	upd := domain.TeamUpdate{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("team: name must not be blank")
		}
		upd.Name = &name
	}
	if in.WorkspaceDomain != nil {
		dom := domain.NormalizeWorkspaceDomain(*in.WorkspaceDomain)
		if err := domain.ValidateWorkspaceDomain(dom); err != nil {
			return nil, fmt.Errorf("team: invalid workspace_domain")
		}
		upd.WorkspaceDomain = &dom
	}
	return s.teams.Update(ctx, id, upd)
}

// Delete removes the team. The coordinated transaction (member-row
// removal + session revocation + org-admin team_id NULL-out) is owed
// by the next commit — for now this is a thin wrapper over the repo's
// raw DELETE, which the FK constraints will reject if dependent rows
// exist. ErrNotFound for unknown ids.
//
// TODO(US2 team-deletion orchestration): wrap in a transaction that
// (a) selects every member-role membership for this team_id, deletes
// those rows + revokes their sessions with reason team-deleted;
// (b) UPDATEs any org-admin rows pointing at this team_id to NULL;
// (c) DELETEs the team. The repo's RESTRICT FK on
// organization_memberships.team_id ensures forgetting (a) surfaces
// loudly rather than silently violating the member-requires-team CHECK.
func (s *TeamService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.teams.Delete(ctx, id)
}
