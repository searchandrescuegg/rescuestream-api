package acl

import (
	"math/rand/v2"
	"testing"

	"github.com/google/uuid"
)

// helpers ---------------------------------------------------------------

func mustUUID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewRandom()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }

// matchRule --------------------------------------------------------------

func TestMatchRule_user(t *testing.T) {
	userID := mustUUID(t)
	other := mustUUID(t)
	a := Attrs{UserID: userID}

	if !matchRule(Rule{Type: RuleTypeUser, Target: userID}, a) {
		t.Fatal("expected user match")
	}
	if matchRule(Rule{Type: RuleTypeUser, Target: other}, a) {
		t.Fatal("expected user mismatch on different target")
	}
}

func TestMatchRule_team(t *testing.T) {
	team := mustUUID(t)
	a := Attrs{TeamID: &team}

	if !matchRule(Rule{Type: RuleTypeTeam, Target: team}, a) {
		t.Fatal("expected team match")
	}
	if matchRule(Rule{Type: RuleTypeTeam, Target: mustUUID(t)}, Attrs{TeamID: &team}) {
		t.Fatal("expected team mismatch on different target")
	}
	if matchRule(Rule{Type: RuleTypeTeam, Target: team}, Attrs{TeamID: nil}) {
		t.Fatal("expected no match when caller has no team")
	}
}

func TestMatchRule_tag(t *testing.T) {
	t1 := mustUUID(t)
	t2 := mustUUID(t)
	t3 := mustUUID(t)
	a := Attrs{TagIDs: []uuid.UUID{t1, t2}}

	if !matchRule(Rule{Type: RuleTypeTag, Target: t1}, a) {
		t.Fatal("expected tag match on t1")
	}
	if !matchRule(Rule{Type: RuleTypeTag, Target: t2}, a) {
		t.Fatal("expected tag match on t2")
	}
	if matchRule(Rule{Type: RuleTypeTag, Target: t3}, a) {
		t.Fatal("expected mismatch on unassigned tag")
	}
	if matchRule(Rule{Type: RuleTypeTag, Target: t1}, Attrs{}) {
		t.Fatal("expected no match when caller has no tags")
	}
}

func TestMatchRule_unknownType(t *testing.T) {
	if matchRule(Rule{Type: "unknown", Target: mustUUID(t)}, Attrs{UserID: mustUUID(t)}) {
		t.Fatal("unknown rule types must not match")
	}
}

// Evaluate ---------------------------------------------------------------

func TestEvaluate_emptyAnd(t *testing.T) {
	if !Evaluate(RuleSet{Combinator: CombAnd}, Attrs{UserID: mustUUID(t)}) {
		t.Fatal("empty AND must be vacuously true")
	}
}

func TestEvaluate_emptyOr(t *testing.T) {
	if Evaluate(RuleSet{Combinator: CombOr}, Attrs{UserID: mustUUID(t)}) {
		t.Fatal("empty OR must be vacuously false")
	}
}

func TestEvaluate_andAllMatch(t *testing.T) {
	team := mustUUID(t)
	tag := mustUUID(t)
	a := Attrs{UserID: mustUUID(t), TeamID: &team, TagIDs: []uuid.UUID{tag}}

	rs := RuleSet{
		Combinator: CombAnd,
		Rules: []Rule{
			{Type: RuleTypeTeam, Target: team},
			{Type: RuleTypeTag, Target: tag},
		},
	}
	if !Evaluate(rs, a) {
		t.Fatal("expected AND to pass when every rule matches")
	}
}

func TestEvaluate_andOneMiss(t *testing.T) {
	team := mustUUID(t)
	tag := mustUUID(t)
	a := Attrs{UserID: mustUUID(t), TeamID: &team, TagIDs: []uuid.UUID{tag}}

	rs := RuleSet{
		Combinator: CombAnd,
		Rules: []Rule{
			{Type: RuleTypeTeam, Target: team},
			{Type: RuleTypeTag, Target: mustUUID(t)}, // tag the caller doesn't have
		},
	}
	if Evaluate(rs, a) {
		t.Fatal("expected AND to fail when one rule misses")
	}
}

func TestEvaluate_orAnyMatch(t *testing.T) {
	team := mustUUID(t)
	a := Attrs{UserID: mustUUID(t), TeamID: &team}

	rs := RuleSet{
		Combinator: CombOr,
		Rules: []Rule{
			{Type: RuleTypeTag, Target: mustUUID(t)}, // miss
			{Type: RuleTypeTeam, Target: team},       // hit
		},
	}
	if !Evaluate(rs, a) {
		t.Fatal("expected OR to pass when any rule matches")
	}
}

func TestEvaluate_orNoMatch(t *testing.T) {
	a := Attrs{UserID: mustUUID(t)}

	rs := RuleSet{
		Combinator: CombOr,
		Rules: []Rule{
			{Type: RuleTypeTeam, Target: mustUUID(t)},
			{Type: RuleTypeTag, Target: mustUUID(t)},
			{Type: RuleTypeUser, Target: mustUUID(t)},
		},
	}
	if Evaluate(rs, a) {
		t.Fatal("expected OR to fail when no rule matches")
	}
}

// Access -----------------------------------------------------------------

func TestAccess_superAdminBypassesEverything(t *testing.T) {
	if !Access(RoleSuperAdmin, Attrs{}, ScopeTeam, ptr(mustUUID(t)), RuleSet{Combinator: CombAnd}) {
		t.Fatal("super-admin must always be allowed")
	}
}

func TestAccess_orgAdminBypassesTeamScopeAndACL(t *testing.T) {
	teamRoom := mustUUID(t)
	adminAttrs := Attrs{UserID: mustUUID(t)} // no team, no tags
	if !Access(RoleOrgAdmin, adminAttrs, ScopeTeam, &teamRoom, RuleSet{
		Combinator: CombAnd,
		Rules:      []Rule{{Type: RuleTypeTag, Target: mustUUID(t)}},
	}) {
		t.Fatal("org-admin must pass team-scope and ACL gates unconditionally")
	}
}

func TestAccess_memberTeamScope_mismatch(t *testing.T) {
	roomTeam := mustUUID(t)
	memberTeam := mustUUID(t)
	a := Attrs{UserID: mustUUID(t), TeamID: &memberTeam}

	if Access(RoleMember, a, ScopeTeam, &roomTeam, RuleSet{}) {
		t.Fatal("team-scoped room must deny members not on the scoped team")
	}
}

func TestAccess_memberTeamScope_noTeam(t *testing.T) {
	roomTeam := mustUUID(t)
	a := Attrs{UserID: mustUUID(t)} // nil TeamID
	if Access(RoleMember, a, ScopeTeam, &roomTeam, RuleSet{}) {
		t.Fatal("team-scoped room must deny members with no team affiliation")
	}
}

func TestAccess_memberTeamScope_match_emptyACL(t *testing.T) {
	roomTeam := mustUUID(t)
	a := Attrs{UserID: mustUUID(t), TeamID: &roomTeam}

	if !Access(RoleMember, a, ScopeTeam, &roomTeam, RuleSet{}) {
		t.Fatal("team-scoped room with empty ACL must allow members of the scoped team")
	}
}

func TestAccess_memberOrgScope_emptyACL(t *testing.T) {
	a := Attrs{UserID: mustUUID(t)}
	if !Access(RoleMember, a, ScopeOrg, nil, RuleSet{}) {
		t.Fatal("org-scoped room with empty ACL is public within the org")
	}
}

func TestAccess_memberOrgScope_aclGates(t *testing.T) {
	targetTag := mustUUID(t)
	a := Attrs{UserID: mustUUID(t), TagIDs: []uuid.UUID{targetTag}}
	aNoTag := Attrs{UserID: mustUUID(t)}

	rs := RuleSet{
		Combinator: CombOr,
		Rules:      []Rule{{Type: RuleTypeTag, Target: targetTag}},
	}
	if !Access(RoleMember, a, ScopeOrg, nil, rs) {
		t.Fatal("expected tag-carrying member to pass OR gate")
	}
	if Access(RoleMember, aNoTag, ScopeOrg, nil, rs) {
		t.Fatal("expected untagged member to fail OR gate")
	}
}

// Property: a stricter AND rule set never admits more than the OR of the same
// rules. This is the classic boolean subset invariant; a regression in the
// combinator logic would violate it.
func TestEvaluate_andIsSubsetOfOr(t *testing.T) {
	team := mustUUID(t)
	tag := mustUUID(t)
	rnd := rand.New(rand.NewPCG(1, 2))

	for range 200 {
		a := Attrs{
			UserID: mustUUID(t),
			TagIDs: []uuid.UUID{},
		}
		if rnd.IntN(2) == 0 {
			a.TeamID = &team
		}
		if rnd.IntN(2) == 0 {
			a.TagIDs = append(a.TagIDs, tag)
		}

		rules := []Rule{
			{Type: RuleTypeTeam, Target: team},
			{Type: RuleTypeTag, Target: tag},
		}

		and := Evaluate(RuleSet{Combinator: CombAnd, Rules: rules}, a)
		or := Evaluate(RuleSet{Combinator: CombOr, Rules: rules}, a)

		if and && !or {
			t.Fatalf("invariant broken: AND admitted attrs %+v that OR rejected", a)
		}
	}
}
