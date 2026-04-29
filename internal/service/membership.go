package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// MembershipService implements the auto-join flow (FR-008, FR-009) and
// will host the broader membership lifecycle as US2 grows.
//
// Auto-join precedence (spec edge case "Org-admin whose email's domain
// matches a team in a different org"): if the user already holds any
// organization_memberships row at all — regardless of org or role — that
// row is preserved and the domain-based lookup is skipped. This keeps
// explicit super-admin-driven assignments authoritative over passive
// Workspace-domain auto-join.
type MembershipService struct {
	users       domain.UserRepository
	teams       domain.TeamRepository
	memberships domain.MembershipRepository
}

// NewMembershipService constructs a MembershipService.
func NewMembershipService(
	users domain.UserRepository,
	teams domain.TeamRepository,
	memberships domain.MembershipRepository,
) *MembershipService {
	return &MembershipService{users: users, teams: teams, memberships: memberships}
}

// AutoJoinInput carries the already-validated Google identity for the
// auto-join flow. The caller (POST /sessions/login-complete handler in
// US2's next commit) is responsible for verifying the id_token before
// passing these fields in.
type AutoJoinInput struct {
	GoogleSubject string
	Email         string
	DisplayName   string // optional; passed when present in the Google profile
	AvatarURL     string // optional; passed when present in the Google profile
}

// AutoJoinResult is what AutoJoinFromGoogle returns. Membership is nil
// when the user has no organization affiliation after the call —
// either because their email domain doesn't match any team and they had
// no prior membership, or because the team-lookup found nothing.
//
// MembershipChanged reports whether this call inserted or replaced a
// membership row. False means the user's membership state is unchanged
// (preserved from a prior assignment, OR remained "no membership").
// The eventual audit-emission glue uses this to skip no-op events.
type AutoJoinResult struct {
	User              *domain.User
	Membership        *domain.OrganizationMembership
	MembershipChanged bool
}

// AutoJoinFromGoogle upserts the user by Google subject + email and,
// if they don't already hold a membership, looks up a team by their
// email's domain and creates a member-role membership in that team's
// organization.
//
// Failure modes:
//   - blank email or google_subject → wrapped validation error;
//   - user upsert / repo errors propagate wrapped.
func (s *MembershipService) AutoJoinFromGoogle(ctx context.Context, in AutoJoinInput) (*AutoJoinResult, error) {
	email := domain.NormalizeEmail(in.Email)
	if email == "" {
		return nil, fmt.Errorf("auto_join: email required")
	}
	if strings.TrimSpace(in.GoogleSubject) == "" {
		return nil, fmt.Errorf("auto_join: google_subject required")
	}

	upsert := domain.UserUpsert{
		Email:         email,
		GoogleSubject: ptrIfNonEmpty(in.GoogleSubject),
		DisplayName:   ptrIfNonEmpty(in.DisplayName),
		AvatarURL:     ptrIfNonEmpty(in.AvatarURL),
	}
	userID, err := s.users.Upsert(ctx, upsert)
	if err != nil {
		return nil, fmt.Errorf("auto_join: upsert user: %w", err)
	}
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("auto_join: refetch user: %w", err)
	}

	// Existing-membership precedence: any prior membership row wins
	// over the domain-based auto-join, regardless of org or role.
	existing, err := s.memberships.GetByUser(ctx, userID)
	if err == nil {
		return &AutoJoinResult{User: user, Membership: existing, MembershipChanged: false}, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("auto_join: lookup membership: %w", err)
	}

	// No existing membership → try to find a team owning the user's
	// email domain.
	dom := emailDomain(email)
	if dom == "" {
		return &AutoJoinResult{User: user}, nil
	}
	team, err := s.teams.FindByWorkspaceDomain(ctx, dom)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// No team owns this domain → user remains in the
			// awaiting-access state (FR-029).
			return &AutoJoinResult{User: user}, nil
		}
		return nil, fmt.Errorf("auto_join: team lookup: %w", err)
	}

	teamID := team.ID
	m, err := s.memberships.Replace(ctx, domain.MembershipReplace{
		UserID:         userID,
		OrganizationID: team.OrganizationID,
		TeamID:         &teamID,
		Role:           domain.MembershipRoleMember,
	})
	if err != nil {
		return nil, fmt.Errorf("auto_join: replace membership: %w", err)
	}

	// TODO(T048 / T055 audit): emit a `membership.granted` audit event
	// here when the v2 audit vocabulary lands. Skipped for now so this
	// commit doesn't double up on T048's design.

	return &AutoJoinResult{User: user, Membership: m, MembershipChanged: true}, nil
}

// emailDomain returns the lowercased domain portion of an email
// (whatever follows the LAST '@'). Returns the empty string if the
// input doesn't contain a domain (no '@', or '@' is the trailing char).
func emailDomain(email string) string {
	i := strings.LastIndex(email, "@")
	if i < 0 || i == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[i+1:])
}

// ptrIfNonEmpty returns &s when s != "", else nil. The user repo's
// upsert paths use COALESCE on these optional fields, so a nil pointer
// preserves the prior value rather than blanking it.
func ptrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
