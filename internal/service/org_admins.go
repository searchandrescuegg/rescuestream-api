package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// OrgAdminsService implements FR-004 — assigning and removing
// organization admins by email.
//
// AddAdmin reassigns: a user already in some other organization is
// removed from there (Replace overwrites the UNIQUE(user_id) row),
// becomes an org-admin of the target org, and has all of their active
// sessions revoked so the next request observes their new identity.
//
// RemoveAdmin only operates on memberships where the user is currently
// an org-admin of the target org; member roles are not affected. After
// the removal the user has no membership at all (FR-002 forbids
// holding two), so they will see /problems/no-org-membership on their
// next request — until they sign in again and auto-join via Workspace
// domain (FR-008).
type OrgAdminsService struct {
	members  domain.MembershipRepository
	users    domain.UserRepository
	orgs     domain.OrganizationRepository
	sessions *SessionService
	logger   *slog.Logger
}

// OrgAdminsOption configures an OrgAdminsService at construction time.
type OrgAdminsOption func(*OrgAdminsService)

// WithOrgAdminsLogger overrides the service logger.
func WithOrgAdminsLogger(logger *slog.Logger) OrgAdminsOption {
	return func(s *OrgAdminsService) { s.logger = logger }
}

// NewOrgAdminsService constructs an OrgAdminsService.
func NewOrgAdminsService(
	members domain.MembershipRepository,
	users domain.UserRepository,
	orgs domain.OrganizationRepository,
	sessions *SessionService,
	opts ...OrgAdminsOption,
) *OrgAdminsService {
	s := &OrgAdminsService{
		members:  members,
		users:    users,
		orgs:     orgs,
		sessions: sessions,
		logger:   slog.Default(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// AddAdminInput is the input for AddAdmin.
type AddAdminInput struct {
	OrgID uuid.UUID
	Email string
}

// AddAdmin upserts the target user and replaces their membership with
// an org-admin row in the target org. Active sessions are revoked
// (best-effort; failure is logged, not returned, because the auth
// pipeline's per-request identity resolution will observe the new
// membership on the next request anyway).
//
// Returns:
//   - domain.ErrNotFound if OrgID does not exist.
//   - validation error for blank email.
func (s *OrgAdminsService) AddAdmin(ctx context.Context, in AddAdminInput) (*domain.OrganizationMembership, error) {
	if in.OrgID == uuid.Nil {
		return nil, fmt.Errorf("org_admins: org_id required")
	}
	email := domain.NormalizeEmail(in.Email)
	if email == "" {
		return nil, fmt.Errorf("org_admins: email required")
	}

	// Verify the target org exists. If it doesn't, we don't want to
	// upsert a user just to fail at the membership insert with a FK
	// violation.
	if _, err := s.orgs.FindByID(ctx, in.OrgID); err != nil {
		return nil, err
	}

	userID, err := s.users.Upsert(ctx, domain.UserUpsert{Email: email})
	if err != nil {
		return nil, fmt.Errorf("org_admins.AddAdmin: upsert user: %w", err)
	}

	m, err := s.members.Replace(ctx, domain.MembershipReplace{
		UserID:         userID,
		OrganizationID: in.OrgID,
		TeamID:         nil,
		Role:           domain.MembershipRoleOrgAdmin,
	})
	if err != nil {
		return nil, fmt.Errorf("org_admins.AddAdmin: replace membership: %w", err)
	}

	if _, revokeErr := s.sessions.RevokeAllForUser(ctx, userID, domain.SessionRevokeReasonRoleChanged); revokeErr != nil {
		s.logger.Warn("org_admins: session revocation failed (non-fatal)",
			slog.String("user_id", userID.String()),
			slog.String("org_id", in.OrgID.String()),
			slog.String("error", revokeErr.Error()),
		)
	}

	return m, nil
}

// RevokeMemberSessions implements FR-030b force-logout: an org-admin
// (or super-admin) bulk-revokes every active session for a user who
// is currently a member of orgID. The membership row is preserved —
// only sessions are invalidated. Returns the count of sessions
// transitioned (0 if the user already had no active sessions).
//
// Returns domain.ErrNotFound if the target user isn't currently
// holding a membership in orgID. This avoids leaking the user's
// actual org affiliation across tenants: an org-admin probing
// "/orgs/<theirs>/members/<random-uuid>/revoke-sessions" gets the
// same 404 whether the user doesn't exist OR exists in a different
// org.
//
// Super-admins are not subject to the org-membership check at this
// service-level helper; the handler enforces caller authorization
// before invoking. (Super-admins still need the user to be in *some*
// org for this route to make URL sense; "force-revoke any user
// platform-wide" is a future endpoint.)
func (s *OrgAdminsService) RevokeMemberSessions(ctx context.Context, orgID, userID uuid.UUID) (int64, error) {
	if orgID == uuid.Nil {
		return 0, fmt.Errorf("org_admins: org_id required")
	}
	if userID == uuid.Nil {
		return 0, fmt.Errorf("org_admins: user_id required")
	}

	m, err := s.members.GetByUser(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return 0, domain.ErrNotFound
		}
		return 0, fmt.Errorf("org_admins.RevokeMemberSessions: get membership: %w", err)
	}
	if m.OrganizationID != orgID {
		return 0, domain.ErrNotFound
	}

	n, err := s.sessions.RevokeAllForUser(ctx, userID, domain.SessionRevokeReasonAdminForceLogout)
	if err != nil {
		return 0, fmt.Errorf("org_admins.RevokeMemberSessions: revoke: %w", err)
	}
	return n, nil
}

// RemoveAdmin removes the user's org-admin membership in the target
// org. Returns domain.ErrNotFound if the user isn't currently an
// org-admin of that specific org. Active sessions are revoked
// (best-effort, same rationale as AddAdmin).
func (s *OrgAdminsService) RemoveAdmin(ctx context.Context, orgID, userID uuid.UUID) error {
	if orgID == uuid.Nil {
		return fmt.Errorf("org_admins: org_id required")
	}
	if userID == uuid.Nil {
		return fmt.Errorf("org_admins: user_id required")
	}

	m, err := s.members.GetByUser(ctx, userID)
	if err != nil {
		// Treat any lookup failure (including ErrNotFound) as the
		// user not being an admin of this org.
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrNotFound
		}
		return fmt.Errorf("org_admins.RemoveAdmin: get membership: %w", err)
	}
	if m.OrganizationID != orgID || m.Role != domain.MembershipRoleOrgAdmin {
		// Caller is asking about a (org, user) pair that doesn't match
		// the user's current admin row. Surface as ErrNotFound rather
		// than leak the user's actual org affiliation across tenants.
		return domain.ErrNotFound
	}

	if err := s.members.DeleteByUser(ctx, userID); err != nil {
		return fmt.Errorf("org_admins.RemoveAdmin: delete membership: %w", err)
	}

	if _, revokeErr := s.sessions.RevokeAllForUser(ctx, userID, domain.SessionRevokeReasonMembershipRemoved); revokeErr != nil {
		s.logger.Warn("org_admins: session revocation failed (non-fatal)",
			slog.String("user_id", userID.String()),
			slog.String("org_id", orgID.String()),
			slog.String("error", revokeErr.Error()),
		)
	}

	return nil
}
