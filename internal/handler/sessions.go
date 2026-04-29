package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

// SessionsHandler hosts the session-management endpoints
// (api-routes.md §11):
//
//   - POST /sessions/login-complete — public; trades a Google id_token
//     for a session keypair via LoginService.LoginComplete.
//   - POST /sessions/logout — any-signed-in; revokes the caller's
//     current session via SessionService.Logout.
//
// The two routes have different middleware needs: login-complete is
// public (the body itself is the authentication), and logout MUST work
// for any user with a valid session — including users with no
// organization membership (otherwise no-org users could never log out).
// The server wires them with separate middleware chains.
type SessionsHandler struct {
	login    *service.LoginService
	sessions *service.SessionService
	logger   *slog.Logger
}

// NewSessionsHandler constructs the handler.
func NewSessionsHandler(login *service.LoginService, sessions *service.SessionService, logger *slog.Logger) *SessionsHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SessionsHandler{login: login, sessions: sessions, logger: logger}
}

// --- Wire shapes -----------------------------------------------------

type loginCompleteRequest struct {
	IDToken string `json:"id_token"`
}

// loginCompleteResponse mirrors api-routes.md §11. The session_secret
// is plaintext and is returned EXACTLY ONCE (at mint time); subsequent
// requests use the same secret to compute X-Signature.
type loginCompleteResponse struct {
	SessionKeyID  string     `json:"session_key_id"`
	SessionSecret string     `json:"session_secret"`
	ExpiresAt     time.Time  `json:"expires_at"`
	User          *userView  `json:"user"`
	Role          string     `json:"role"`
	OrgID         *uuid.UUID `json:"org_id,omitempty"`
	TeamID        *uuid.UUID `json:"team_id,omitempty"`
}

// userView strips the persistence-side timestamps and pointers from a
// domain.User for the wire shape. Keeps the response surface stable
// even if the user struct grows internal fields.
type userView struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
}

func toUserView(u *domain.User) *userView {
	if u == nil {
		return nil
	}
	out := &userView{ID: u.ID, Email: u.Email}
	if u.DisplayName != nil {
		out.DisplayName = *u.DisplayName
	}
	if u.AvatarURL != nil {
		out.AvatarURL = *u.AvatarURL
	}
	return out
}

// --- Handlers --------------------------------------------------------

// LoginComplete handles POST /sessions/login-complete.
func (h *SessionsHandler) LoginComplete(w http.ResponseWriter, r *http.Request) {
	var req loginCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, r, ErrInvalidRequest("Invalid JSON body"))
		return
	}
	if req.IDToken == "" {
		WriteError(w, r, ErrInvalidRequest("`id_token` is required"))
		return
	}

	res, err := h.login.LoginComplete(r.Context(), service.LoginCompleteInput{
		IDToken:   req.IDToken,
		ClientIP:  clientIPForLog(r),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		// id_token verification failures must surface as 401 — they're
		// the caller's authentication problem, not a server fault.
		// Anything that mentions "verification" or "id_token" in the
		// error chain falls into that bucket; everything else is 500.
		if isAuthError(err) {
			h.logger.Warn("login id_token rejected",
				slog.String("error", err.Error()),
				slog.String("remote_addr", r.RemoteAddr),
			)
			WriteError(w, r, ErrUnauthorized("Invalid Google id_token"))
			return
		}
		h.logger.Error("login complete failed",
			slog.String("error", err.Error()),
			slog.String("remote_addr", r.RemoteAddr),
		)
		WriteError(w, r, ErrInternalServer("Failed to complete login"))
		return
	}

	WriteJSON(w, http.StatusCreated, loginCompleteResponse{
		SessionKeyID:  res.SessionKeyID,
		SessionSecret: res.SessionSecret,
		ExpiresAt:     res.ExpiresAt,
		User:          toUserView(res.User),
		Role:          res.Role,
		OrgID:         res.OrgID,
		TeamID:        res.TeamID,
	})
}

// Logout handles POST /sessions/logout. Requires a valid session
// (caller's session_id is read from the request context, populated by
// AuthMiddleware). Identity resolution is intentionally NOT required:
// no-org-membership users are still allowed to log out.
func (h *SessionsHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sessionID := SessionIDFromContext(r.Context())
	if sessionID == uuid.Nil {
		WriteError(w, r, ErrUnauthorized("No active session"))
		return
	}
	if err := h.sessions.Logout(r.Context(), sessionID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Already gone; treat as success.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.logger.Error("logout failed",
			slog.String("error", err.Error()),
			slog.String("session_id", sessionID.String()),
		)
		WriteError(w, r, ErrInternalServer("Failed to log out"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clientIPForLog picks the best-available client IP for forensics.
// Falls back to RemoteAddr if no proxy headers are present. Reuses the
// existing getClientIP helper but keeps a separate name so future
// header tightening (e.g. "trust the first XFF entry only when behind
// a known proxy") doesn't fan out.
func clientIPForLog(r *http.Request) string {
	return getClientIP(r)
}

// isAuthError reports whether err originated from id_token verification.
// We classify by error-message substring because the underlying lib
// doesn't expose typed errors; if/when it does we can switch to errors.Is.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, marker := range []string{
		"id_token verification",
		"oauth.Verify",
		"id_token required",
	} {
		if containsCI(msg, marker) {
			return true
		}
	}
	return false
}

// containsCI is a tiny case-insensitive contains. Pulled out so the
// auth-error classifier doesn't drag in strings.ToLower allocations
// on every successful login (it's only invoked on the error path,
// but still).
func containsCI(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a := s[i+j]
			b := sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
