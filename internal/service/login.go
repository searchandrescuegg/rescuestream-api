package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/oauth"
)

// LoginService orchestrates the v2 sign-in flow:
//
//  1. Verify the Google id_token (signature, audience, expiry,
//     email_verified) via the injected GoogleIDTokenVerifier.
//  2. Auto-join: upsert the user, then preserve any existing
//     organization membership OR create a new member-role membership
//     for a Workspace-domain match (FR-008 / FR-009).
//  3. Resolve the caller's identity (super-admin / org-admin / member /
//     no-membership) so the response can carry the right role + org_id
//     without the frontend needing a follow-up call.
//  4. Mint a session via SessionService — the resulting key_id +
//     signing_key is returned to the caller exactly once.
//
// FR-030a (server-side session store) requires that the resulting
// session be invalidatable from server-side; the Mint path persists a
// row in `sessions` so admin-initiated revocation works on the next
// request.
type LoginService struct {
	verifier oauth.GoogleIDTokenVerifier
	members  *MembershipService
	identity *IdentityResolver
	sessions *SessionService
	logger   *slog.Logger
}

// LoginOption configures a LoginService at construction time.
type LoginOption func(*LoginService)

// WithLoginLogger overrides the service logger.
func WithLoginLogger(logger *slog.Logger) LoginOption {
	return func(s *LoginService) { s.logger = logger }
}

// NewLoginService constructs a LoginService.
func NewLoginService(
	verifier oauth.GoogleIDTokenVerifier,
	members *MembershipService,
	identity *IdentityResolver,
	sessions *SessionService,
	opts ...LoginOption,
) *LoginService {
	s := &LoginService{
		verifier: verifier,
		members:  members,
		identity: identity,
		sessions: sessions,
		logger:   slog.Default(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// LoginCompleteInput carries the inputs for a sign-in request.
type LoginCompleteInput struct {
	IDToken   string
	ClientIP  string // optional; recorded on the session row for forensics
	UserAgent string // optional; recorded on the session row for forensics
}

// LoginCompleteResult is what LoginComplete returns. SessionSecret is
// plaintext and is the ONLY opportunity for the caller to capture it —
// the server discards it after Mint. Role is one of "super-admin",
// "org-admin", "member", or "none" (no organization affiliation yet).
// OrgID + TeamID are nil for super-admins without a membership and for
// no-org users.
type LoginCompleteResult struct {
	SessionKeyID  string
	SessionSecret string
	ExpiresAt     time.Time
	User          *domain.User
	Role          string
	OrgID         *uuid.UUID
	TeamID        *uuid.UUID
}

// LoginComplete executes the four-step sign-in pipeline.
//
// Failure modes:
//   - empty id_token / verifier failure → wrapped error (handler maps to 401).
//   - downstream service failures propagate wrapped (handler maps to 500).
func (s *LoginService) LoginComplete(ctx context.Context, in LoginCompleteInput) (*LoginCompleteResult, error) {
	if in.IDToken == "" {
		return nil, fmt.Errorf("login: id_token required")
	}

	claims, err := s.verifier.Verify(ctx, in.IDToken)
	if err != nil {
		return nil, fmt.Errorf("login: id_token verification: %w", err)
	}

	join, err := s.members.AutoJoinFromGoogle(ctx, AutoJoinInput{
		GoogleSubject: claims.Subject,
		Email:         claims.Email,
		DisplayName:   claims.DisplayName,
		AvatarURL:     claims.AvatarURL,
	})
	if err != nil {
		return nil, fmt.Errorf("login: auto-join: %w", err)
	}

	mint, err := s.sessions.Mint(ctx, MintInput{
		UserID:    join.User.ID,
		ClientIP:  in.ClientIP,
		UserAgent: in.UserAgent,
	})
	if err != nil {
		return nil, fmt.Errorf("login: mint session: %w", err)
	}

	role := "none"
	var orgID, teamID *uuid.UUID

	ident, idErr := s.identity.Resolve(ctx, join.User.ID)
	switch {
	case idErr == nil:
		role = string(ident.Role)
		if ident.OrgID != uuid.Nil {
			id := ident.OrgID
			orgID = &id
		}
		if ident.TeamID != nil {
			id := *ident.TeamID
			teamID = &id
		}
	case errors.Is(idErr, domain.ErrNoOrgMembership):
		// No-membership user — let them complete sign-in but flag the
		// state so the frontend can show the awaiting-access message.
		role = "none"
	default:
		// An unexpected resolver error AFTER session mint isn't fatal
		// for the user — they have a session and can retry — but log
		// it loudly so we notice the resolver failing intermittently.
		s.logger.Warn("login: identity resolution failed after session mint",
			slog.String("user_id", join.User.ID.String()),
			slog.String("error", idErr.Error()),
		)
	}

	return &LoginCompleteResult{
		SessionKeyID:  mint.KeyID,
		SessionSecret: mint.SigningKey,
		ExpiresAt:     mint.Session.ExpiresAt,
		User:          join.User,
		Role:          role,
		OrgID:         orgID,
		TeamID:        teamID,
	}, nil
}
