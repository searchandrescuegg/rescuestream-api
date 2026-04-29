// Package acl implements the pure, DB-free room-access evaluator documented
// in specs/003-multi-tenant-platform/research.md §8.
//
// The evaluator is used on two hot paths:
//   - per-request access checks against a stored ACL.
//   - the ACL-preview endpoint, which counts members a draft rule set would
//     admit.
//
// Both paths load caller attributes once (team_id, tag_ids) and run this
// evaluator in memory; see service/acl for the preview implementation.
package acl

import (
	"slices"

	"github.com/google/uuid"
)

// Role enumerates the three platform role tiers.
type Role string

const (
	RoleSuperAdmin Role = "super-admin"
	RoleOrgAdmin   Role = "org-admin"
	RoleMember     Role = "member"
)

// RoomScope enumerates the two room scopes supported by FR-018.
type RoomScope string

const (
	ScopeOrg  RoomScope = "org"
	ScopeTeam RoomScope = "team"
)

// Combinator enumerates the two rule-set combinators permitted by FR-019.
type Combinator string

const (
	CombAnd Combinator = "and"
	CombOr  Combinator = "or"
)

// RuleType enumerates the three kinds of ACL rule targets permitted by FR-019.
type RuleType string

const (
	RuleTypeTeam RuleType = "team"
	RuleTypeTag  RuleType = "tag"
	RuleTypeUser RuleType = "user"
)

// Attrs are the caller attributes required to evaluate a RuleSet.
type Attrs struct {
	UserID uuid.UUID
	// TeamID is nil for org-admins without a team affiliation (FR-009).
	TeamID *uuid.UUID
	TagIDs []uuid.UUID
}

// Rule is one entry in a room's ACL rule set.
type Rule struct {
	Type   RuleType
	Target uuid.UUID
}

// RuleSet is the collection of rules plus their combinator (FR-019).
type RuleSet struct {
	Combinator Combinator
	Rules      []Rule
}

// Evaluate returns true iff the caller's attributes satisfy the rule set under
// its combinator. An empty rule set is vacuously true under AND and vacuously
// false under OR; callers typically short-circuit on empty rule sets before
// reaching Evaluate (see Access).
func Evaluate(rs RuleSet, a Attrs) bool {
	if rs.Combinator == CombAnd {
		for _, r := range rs.Rules {
			if !matchRule(r, a) {
				return false
			}
		}
		return true
	}
	// OR (default for unknown values; log upstream if this happens)
	for _, r := range rs.Rules {
		if matchRule(r, a) {
			return true
		}
	}
	return false
}

// Access applies the full room-access decision tree per research §8:
//
//  1. super-admins always pass.
//  2. org-admins always pass (tenancy is enforced upstream; by the time we
//     reach this evaluator a non-super-admin caller's org must already match
//     the room's org).
//  3. for a team-scoped room, the caller's team must match the room's team.
//  4. an empty rule set means "public within scope".
//  5. otherwise defer to Evaluate.
//
// The roomTeamID argument is ignored when scope == ScopeOrg; for ScopeTeam it
// MUST be non-nil.
func Access(role Role, attrs Attrs, scope RoomScope, roomTeamID *uuid.UUID, rs RuleSet) bool {
	if role == RoleSuperAdmin || role == RoleOrgAdmin {
		return true
	}
	if scope == ScopeTeam {
		if roomTeamID == nil || attrs.TeamID == nil || *attrs.TeamID != *roomTeamID {
			return false
		}
	}
	if len(rs.Rules) == 0 {
		return true
	}
	return Evaluate(rs, attrs)
}

func matchRule(r Rule, a Attrs) bool {
	switch r.Type {
	case RuleTypeUser:
		return r.Target == a.UserID
	case RuleTypeTeam:
		return a.TeamID != nil && *a.TeamID == r.Target
	case RuleTypeTag:
		return slices.Contains(a.TagIDs, r.Target)
	default:
		return false
	}
}
