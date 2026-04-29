package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

// autoJoinStack composes every dependency the auto-join flow touches
// against testcontainers Postgres.
type autoJoinStack struct {
	td      *testutil.TestDatabase
	svc     *service.MembershipService
	users   *database.UserRepo
	teams   *database.TeamRepo
	members *database.MembershipRepo
	orgs    *database.OrganizationRepo
}

func newAutoJoinStack(t *testing.T) *autoJoinStack {
	t.Helper()
	td := testutil.SetupTestDatabase(t)
	t.Cleanup(func() { td.Cleanup(t) })

	users := database.NewUserRepo(td.Pool)
	teams := database.NewTeamRepo(td.Pool)
	members := database.NewMembershipRepo(td.Pool)
	orgs := database.NewOrganizationRepo(td.Pool)

	svc := service.NewMembershipService(users, teams, members)

	return &autoJoinStack{td: td, svc: svc, users: users, teams: teams, members: members, orgs: orgs}
}

// makeOrgWithCreator creates an organization (returning its id) plus a
// throwaway creator user to satisfy the FK.
func (st *autoJoinStack) makeOrgWithCreator(t *testing.T, slug string) uuid.UUID {
	t.Helper()
	creator, err := st.users.Upsert(context.Background(), domain.UserUpsert{
		Email: "creator-" + slug + "@example.org",
	})
	require.NoError(t, err)
	org, err := st.orgs.Create(context.Background(), domain.OrganizationCreate{
		Name: "Org " + slug, Slug: slug, CreatedByUserID: creator,
	})
	require.NoError(t, err)
	return org.ID
}

// makeTeam creates a team in the given org with the given workspace_domain.
func (st *autoJoinStack) makeTeam(t *testing.T, orgID uuid.UUID, name, dom string) *domain.Team {
	t.Helper()
	team, err := st.teams.Create(context.Background(), domain.TeamCreate{
		OrganizationID: orgID, Name: name, WorkspaceDomain: dom,
	})
	require.NoError(t, err)
	return team
}

// --- Tests -----------------------------------------------------------

func TestAutoJoinFromGoogle_NewUserMatchingTeam_CreatesMemberMembership(t *testing.T) {
	st := newAutoJoinStack(t)
	orgID := st.makeOrgWithCreator(t, "auto-match")
	team := st.makeTeam(t, orgID, "Engineering", "match.example.org")

	got, err := st.svc.AutoJoinFromGoogle(context.Background(), service.AutoJoinInput{
		GoogleSubject: "google-sub-001",
		Email:         "Alice@match.example.org", // mixed case → must normalize
		DisplayName:   "Alice",
		AvatarURL:     "https://example.org/a.png",
	})
	require.NoError(t, err)
	require.NotNil(t, got.User)
	require.NotNil(t, got.Membership)
	assert.True(t, got.MembershipChanged)

	assert.Equal(t, "alice@match.example.org", got.User.Email, "email must be normalized")
	require.NotNil(t, got.User.GoogleSubject)
	assert.Equal(t, "google-sub-001", *got.User.GoogleSubject)
	require.NotNil(t, got.User.DisplayName)
	assert.Equal(t, "Alice", *got.User.DisplayName)

	assert.Equal(t, orgID, got.Membership.OrganizationID)
	require.NotNil(t, got.Membership.TeamID)
	assert.Equal(t, team.ID, *got.Membership.TeamID)
	assert.Equal(t, domain.MembershipRoleMember, got.Membership.Role)
}

func TestAutoJoinFromGoogle_NewUserNoMatchingTeam_LeavesNoMembership(t *testing.T) {
	st := newAutoJoinStack(t)
	orgID := st.makeOrgWithCreator(t, "auto-no-match")
	st.makeTeam(t, orgID, "Engineering", "engineering.example.org")

	// User's email domain doesn't match any team — no auto-join
	// (FR-029 awaiting-access state on next request).
	got, err := st.svc.AutoJoinFromGoogle(context.Background(), service.AutoJoinInput{
		GoogleSubject: "google-sub-002",
		Email:         "bob@unknown-domain.example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, got.User)
	assert.Nil(t, got.Membership, "no team match → no membership")
	assert.False(t, got.MembershipChanged)

	// Confirm no row was created.
	_, err = st.members.GetByUser(context.Background(), got.User.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestAutoJoinFromGoogle_PreservesExistingMembership_FR009Precedence(t *testing.T) {
	// Spec edge case: "Org-admin whose email's domain matches a team in
	// a different org" — the user's existing membership wins. We model
	// this generically: ANY existing membership row preserves itself.
	st := newAutoJoinStack(t)
	orgA := st.makeOrgWithCreator(t, "fr009-a")
	orgB := st.makeOrgWithCreator(t, "fr009-b")

	// Bob is already an org-admin of orgA.
	bobID, err := st.users.Upsert(context.Background(), domain.UserUpsert{
		Email: "bob@orgb.example.org", // email matches orgB's team domain
	})
	require.NoError(t, err)
	prior, err := st.members.Replace(context.Background(), domain.MembershipReplace{
		UserID:         bobID,
		OrganizationID: orgA,
		TeamID:         nil,
		Role:           domain.MembershipRoleOrgAdmin,
	})
	require.NoError(t, err)

	// orgB has a team owning bob's email domain — this would normally trigger auto-join.
	st.makeTeam(t, orgB, "OrgB Team", "orgb.example.org")

	// Auto-join must reuse bob's existing org-admin membership in orgA
	// rather than reassigning him to orgB.
	got, err := st.svc.AutoJoinFromGoogle(context.Background(), service.AutoJoinInput{
		GoogleSubject: "google-sub-bob",
		Email:         "bob@orgb.example.org",
	})
	require.NoError(t, err)
	require.NotNil(t, got.Membership)
	assert.False(t, got.MembershipChanged, "must NOT be marked as a change")
	assert.Equal(t, prior.ID, got.Membership.ID, "must reuse the existing row id")
	assert.Equal(t, orgA, got.Membership.OrganizationID, "must remain in orgA")
	assert.Equal(t, domain.MembershipRoleOrgAdmin, got.Membership.Role, "must remain org-admin")
}

func TestAutoJoinFromGoogle_AuditBackfilledUser_UpgradesAndAutoJoins(t *testing.T) {
	// A user backfilled from audit_logs.actor on the v1→v2 cutover has
	// an email-only row with NULL google_subject. On their first
	// Google sign-in, we expect:
	//   - the existing row to be updated (google_subject + display_name fill in)
	//   - auto-join to fire because they had NO membership
	st := newAutoJoinStack(t)
	orgID := st.makeOrgWithCreator(t, "backfill")
	st.makeTeam(t, orgID, "Engineering", "backfill.example.org")

	// Pre-existing email-only row.
	preID, err := st.users.Upsert(context.Background(), domain.UserUpsert{
		Email: "carol@backfill.example.org",
	})
	require.NoError(t, err)

	got, err := st.svc.AutoJoinFromGoogle(context.Background(), service.AutoJoinInput{
		GoogleSubject: "google-sub-carol",
		Email:         "carol@backfill.example.org",
		DisplayName:   "Carol",
	})
	require.NoError(t, err)
	assert.Equal(t, preID, got.User.ID, "must reuse the pre-existing user row")
	require.NotNil(t, got.User.GoogleSubject)
	assert.Equal(t, "google-sub-carol", *got.User.GoogleSubject)
	require.NotNil(t, got.User.DisplayName)
	assert.Equal(t, "Carol", *got.User.DisplayName)
	require.NotNil(t, got.Membership)
	assert.True(t, got.MembershipChanged)
}

func TestAutoJoinFromGoogle_RepeatedCallIsIdempotent(t *testing.T) {
	st := newAutoJoinStack(t)
	orgID := st.makeOrgWithCreator(t, "repeat")
	st.makeTeam(t, orgID, "Engineering", "repeat.example.org")

	in := service.AutoJoinInput{
		GoogleSubject: "google-sub-dave",
		Email:         "dave@repeat.example.org",
	}
	first, err := st.svc.AutoJoinFromGoogle(context.Background(), in)
	require.NoError(t, err)
	require.NotNil(t, first.Membership)
	assert.True(t, first.MembershipChanged)

	// Second call: existing-membership precedence kicks in; no change.
	second, err := st.svc.AutoJoinFromGoogle(context.Background(), in)
	require.NoError(t, err)
	require.NotNil(t, second.Membership)
	assert.False(t, second.MembershipChanged)
	assert.Equal(t, first.Membership.ID, second.Membership.ID,
		"second call must return the same membership row id")
}

func TestAutoJoinFromGoogle_MissingEmailIsValidationError(t *testing.T) {
	st := newAutoJoinStack(t)
	_, err := st.svc.AutoJoinFromGoogle(context.Background(), service.AutoJoinInput{
		GoogleSubject: "google-sub", Email: "  ",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email required")
}

func TestAutoJoinFromGoogle_MissingGoogleSubjectIsValidationError(t *testing.T) {
	st := newAutoJoinStack(t)
	_, err := st.svc.AutoJoinFromGoogle(context.Background(), service.AutoJoinInput{
		GoogleSubject: "", Email: "x@example.org",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "google_subject required")
}

func TestAutoJoinFromGoogle_DomainExtractionRespectsLastAt(t *testing.T) {
	// Defensive: an email with multiple '@' (rare but technically RFC-
	// valid in quoted-local-parts) should split on the LAST one.
	// We don't fully support quoted local parts but the auto-join
	// should at least not crash and should pick the rightmost domain.
	st := newAutoJoinStack(t)
	orgID := st.makeOrgWithCreator(t, "lastat")
	team := st.makeTeam(t, orgID, "Engineering", "right.example.org")

	got, err := st.svc.AutoJoinFromGoogle(context.Background(), service.AutoJoinInput{
		GoogleSubject: "google-sub-quirky",
		Email:         `"quirky@local"@right.example.org`,
	})
	require.NoError(t, err)
	require.NotNil(t, got.Membership)
	require.NotNil(t, got.Membership.TeamID)
	assert.Equal(t, team.ID, *got.Membership.TeamID)
}
