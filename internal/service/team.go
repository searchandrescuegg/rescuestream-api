package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// TeamService is the business-logic boundary for team CRUD. Wraps
// TeamRepository with workspace-domain normalization + validation and
// the lifecycle invariants FR-006 / FR-007 mandate.
//
// Team deletion's coordinated transaction (data-model §1.4 invariants:
// member removal + org-admin team_id NULL-out + team delete, then
// session revocation outside the tx) lives here so handlers stay
// HTTP-shaped. The pool is held directly because the orchestration
// runs raw SQL inside a single tx — the existing repos take the pool
// rather than a Tx, so reusing them inside a tx isn't straightforward.
type TeamService struct {
	pool        *pgxpool.Pool
	teams       domain.TeamRepository
	memberships domain.MembershipRepository
	sessions    *SessionService
	logger      *slog.Logger
}

// TeamOption configures a TeamService at construction time.
type TeamOption func(*TeamService)

// WithTeamLogger overrides the service logger.
func WithTeamLogger(logger *slog.Logger) TeamOption {
	return func(s *TeamService) { s.logger = logger }
}

// NewTeamService constructs a TeamService.
func NewTeamService(
	pool *pgxpool.Pool,
	teams domain.TeamRepository,
	memberships domain.MembershipRepository,
	sessions *SessionService,
	opts ...TeamOption,
) *TeamService {
	s := &TeamService{
		pool:        pool,
		teams:       teams,
		memberships: memberships,
		sessions:    sessions,
		logger:      slog.Default(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
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

// Delete removes the team after running the coordinated transaction
// from data-model §1.4 invariants:
//
//  1. Capture every member-role membership pointing at this team_id
//     (we'll need their user_ids for session revocation after the tx).
//  2. DELETE those member-role membership rows. The CHECK constraint
//     (role='member' ⇒ team_id IS NOT NULL) means we can't NULL them
//     out — the row goes away entirely and the user lands in the
//     awaiting-access state on next sign-in (FR-029).
//  3. UPDATE any org-admin rows pointing at this team_id to NULL.
//     Org-admins keep their admin membership but lose the team
//     affiliation (FR-009).
//  4. DELETE the team.
//  5. Commit.
//  6. Outside the tx (best-effort), force-revoke each removed
//     member's sessions with reason "team-deleted" so a stale token
//     can't keep accessing org-scoped resources after their org
//     affiliation is gone.
//
// The team_id FK on organization_memberships is ON DELETE RESTRICT,
// so step 4 will fail loudly if a member-role row was missed — that's
// the desired floor (better than silently violating the
// member-requires-team CHECK constraint).
//
// Returns domain.ErrNotFound when id is unknown.
func (s *TeamService) Delete(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("team.Delete: id required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("team.Delete: begin tx: %w", err)
	}
	defer func() {
		if rerr := tx.Rollback(ctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			s.logger.Warn("team.Delete: rollback",
				slog.String("team_id", id.String()),
				slog.String("error", rerr.Error()),
			)
		}
	}()

	// First: confirm the team exists. Lets us return a clean ErrNotFound
	// rather than a successful no-op that the caller misreads as "deleted".
	var teamExists bool
	if scanErr := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM teams WHERE id = $1)`, id,
	).Scan(&teamExists); scanErr != nil {
		return fmt.Errorf("team.Delete: existence check: %w", scanErr)
	}
	if !teamExists {
		return domain.ErrNotFound
	}

	// (1) Capture user_ids of every member-role membership pointing at
	// this team — we need them after the tx commits to revoke sessions.
	rows, err := tx.Query(ctx, `
		SELECT user_id
		FROM organization_memberships
		WHERE team_id = $1 AND role = 'member'
	`, id)
	if err != nil {
		return fmt.Errorf("team.Delete: select members: %w", err)
	}
	var memberUserIDs []uuid.UUID
	for rows.Next() {
		var uid uuid.UUID
		if scanErr := rows.Scan(&uid); scanErr != nil {
			rows.Close()
			return fmt.Errorf("team.Delete: scan member: %w", scanErr)
		}
		memberUserIDs = append(memberUserIDs, uid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("team.Delete: iter members: %w", err)
	}

	// (2) Drop the member-role rows.
	if _, err := tx.Exec(ctx, `
		DELETE FROM organization_memberships
		WHERE team_id = $1 AND role = 'member'
	`, id); err != nil {
		return fmt.Errorf("team.Delete: delete member memberships: %w", err)
	}

	// (3) NULL-out any org-admin rows whose team_id happens to match.
	if _, err := tx.Exec(ctx, `
		UPDATE organization_memberships
		SET team_id = NULL
		WHERE team_id = $1 AND role = 'org-admin'
	`, id); err != nil {
		return fmt.Errorf("team.Delete: null org-admin team_id: %w", err)
	}

	// (4) DELETE the team.
	if _, err := tx.Exec(ctx, `DELETE FROM teams WHERE id = $1`, id); err != nil {
		return fmt.Errorf("team.Delete: delete team: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("team.Delete: commit: %w", err)
	}

	// (6) Outside the tx: best-effort session revocation for each
	// removed member. Failures are logged but not returned — the
	// auth pipeline's per-request identity resolution will still
	// observe the missing membership and fail closed on the next
	// request, so a transient revoke failure can't keep a logged-in
	// member-without-team viable.
	for _, uid := range memberUserIDs {
		if _, revErr := s.sessions.RevokeAllForUser(ctx, uid, domain.SessionRevokeReasonTeamDeleted); revErr != nil {
			s.logger.Warn("team.Delete: session revocation (non-fatal)",
				slog.String("team_id", id.String()),
				slog.String("user_id", uid.String()),
				slog.String("error", revErr.Error()),
			)
		}
	}

	return nil
}
