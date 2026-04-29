package domain

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Team is a grouping inside an organization, identified by a unique
// Google Workspace domain (FR-006, FR-007). The workspace_domain
// determines auto-join: a user signing in with a Workspace email whose
// domain matches a team is auto-provisioned as a member of that team.
type Team struct {
	ID              uuid.UUID `json:"id"`
	OrganizationID  uuid.UUID `json:"organization_id"`
	Name            string    `json:"name"`
	WorkspaceDomain string    `json:"workspace_domain"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// workspaceDomainPattern is a permissive but defensive validator for
// Google Workspace domains: lowercase labels separated by dots, each
// label 1–63 chars, and the whole string ≤253 chars (RFC 1035 ish).
// Tighter than "anything with a dot" but loose enough to accept the
// real-world variety of customer domains (single-label TLDs are
// rejected by the leading-label requirement).
var workspaceDomainPattern = regexp.MustCompile(
	`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`,
)

// NormalizeWorkspaceDomain lowercases + trims a workspace domain.
// All persistence and lookups go through this so the UNIQUE index on
// workspace_domain is effectively case-insensitive without a citext
// extension.
func NormalizeWorkspaceDomain(d string) string {
	return strings.ToLower(strings.TrimSpace(d))
}

// ValidateWorkspaceDomain reports nil for valid (already-normalized)
// workspace domain strings, and a non-nil error otherwise. Length cap
// matches the column width in data-model §1.2.
func ValidateWorkspaceDomain(d string) error {
	if len(d) == 0 || len(d) > 253 {
		return ErrInvalidStatus // bad-input sentinel reuse; service layer wraps with detail
	}
	if !workspaceDomainPattern.MatchString(d) {
		return ErrInvalidStatus
	}
	return nil
}

// TeamCreate carries the inputs for inserting a new team.
type TeamCreate struct {
	OrganizationID  uuid.UUID
	Name            string
	WorkspaceDomain string // must be pre-normalized
}

// TeamUpdate carries optional fields for renaming a team or
// re-pointing its workspace domain. Both fields are independent.
type TeamUpdate struct {
	Name            *string
	WorkspaceDomain *string // must be pre-normalized when non-nil
}

// TeamRepository is the persistence boundary for teams.
type TeamRepository interface {
	// Create inserts a new team. Returns ErrWorkspaceDomainTaken if
	// another team (in any org) has already claimed the domain (FR-007).
	Create(ctx context.Context, in TeamCreate) (*Team, error)

	// FindByID returns the team by id, or domain.ErrNotFound.
	FindByID(ctx context.Context, id uuid.UUID) (*Team, error)

	// FindByWorkspaceDomain returns the team that owns the given
	// (normalized) workspace domain, or domain.ErrNotFound. Used by
	// the auto-join flow on every Google sign-in.
	FindByWorkspaceDomain(ctx context.Context, domain string) (*Team, error)

	// ListByOrg returns every team in the org, ordered by name ASC.
	ListByOrg(ctx context.Context, orgID uuid.UUID) ([]Team, error)

	// Update applies a rename and/or a workspace-domain change.
	// ErrWorkspaceDomainTaken if the new domain is already in use.
	// ErrNotFound if the team doesn't exist.
	Update(ctx context.Context, id uuid.UUID, in TeamUpdate) (*Team, error)

	// Delete removes the team. The full team-deletion service
	// orchestration (member removal + session revocation + org-admin
	// team_id NULL-out) lands with the team-handler delete path; this
	// repo method is the raw DELETE the orchestration wraps.
	// ErrNotFound if the team doesn't exist.
	Delete(ctx context.Context, id uuid.UUID) error
}
