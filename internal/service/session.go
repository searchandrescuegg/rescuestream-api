package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/pepper"
)

// MaxTimestampDrift is the largest acceptable skew between an HMAC-signed
// request's X-Timestamp and the server clock.
const MaxTimestampDrift = 5 * time.Minute

// SessionService mints, validates, and revokes server-side sessions.
//
// Wire protocol (research.md §2): every authenticated request carries
//
//	X-API-Key:    <session.hmac_key_id>
//	X-Timestamp:  <unix seconds>
//	X-Signature:  hex(HMAC-SHA256(signing_key, METHOD "\n" PATH "\n" TS "\n" BODY))
//
// Session lifecycle:
//
//   - Mint generates a random `raw` and a stable `signing_key = pepper.Hash(raw)`.
//     The signing_key is returned to the caller (along with the session's
//     hmac_key_id) and is also persisted to sessions.hmac_secret_hash.
//     `raw` itself is discarded immediately. The deterministic peppered hash
//     means the server can later verify HMAC signatures using the stored
//     value as the verification key without retaining plaintext.
//
//   - ValidateSignedRequest looks up the row by hmac_key_id, recomputes the
//     HMAC over the canonical signing string under hmac_secret_hash, and
//     compares to X-Signature in constant time.
//
// For sessions the pepper's primary role is making the stored signing key
// non-trivially derivable from the random plaintext (so a DB dump alone
// reveals values that are useless without the pepper for forging *new*
// sessions of the same identity later — although it does NOT prevent a
// dumped row from being used to forge requests during the session's
// lifetime; the mitigation there is short expiry plus admin-initiated
// revocation per FR-030a/b).
type SessionService struct {
	repo          domain.SessionRepository
	hasher        *pepper.Hasher
	slidingExpiry time.Duration
	keyIDBytes    int
	secretBytes   int
	now           func() time.Time
}

// SessionOption configures a SessionService at construction time.
type SessionOption func(*SessionService)

// WithSlidingExpiry sets the rolling expiry window applied on every
// successful auth (default 30 days).
func WithSlidingExpiry(d time.Duration) SessionOption {
	return func(s *SessionService) { s.slidingExpiry = d }
}

// WithKeyIDBytes sets the entropy of the X-API-Key identifier (default 16
// bytes).
func WithKeyIDBytes(n int) SessionOption {
	return func(s *SessionService) { s.keyIDBytes = n }
}

// WithSecretBytes sets the entropy of the random plaintext that feeds the
// pepper hasher (default 32 bytes).
func WithSecretBytes(n int) SessionOption {
	return func(s *SessionService) { s.secretBytes = n }
}

// WithClock overrides the time source. Test-only.
func WithClock(now func() time.Time) SessionOption {
	return func(s *SessionService) { s.now = now }
}

// NewSessionService constructs a SessionService.
func NewSessionService(repo domain.SessionRepository, hasher *pepper.Hasher, opts ...SessionOption) *SessionService {
	s := &SessionService{
		repo:          repo,
		hasher:        hasher,
		slidingExpiry: 30 * 24 * time.Hour,
		keyIDBytes:    16,
		secretBytes:   32,
		now:           time.Now,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// MintInput carries the inputs for creating a fresh session.
type MintInput struct {
	UserID    uuid.UUID
	ClientIP  string // empty → not recorded
	UserAgent string // empty → not recorded
}

// MintResult carries the credential pair the caller MUST deliver to the
// authenticated user. KeyID becomes X-API-Key; SigningKey becomes the HMAC
// key the user signs each request with. The service does not retain the
// SigningKey after Mint returns.
type MintResult struct {
	Session    *domain.Session
	KeyID      string
	SigningKey string
}

// Mint creates a new session for an already-authenticated user.
//
// Production: invoked from the Google-OAuth callback handler after
// id_token verification + user upsert. Tests call it directly.
func (s *SessionService) Mint(ctx context.Context, in MintInput) (*MintResult, error) {
	keyIDRaw, err := randomBytes(s.keyIDBytes)
	if err != nil {
		return nil, fmt.Errorf("session.Mint: keyID entropy: %w", err)
	}
	rawSecret, err := randomBytes(s.secretBytes)
	if err != nil {
		return nil, fmt.Errorf("session.Mint: secret entropy: %w", err)
	}
	keyID := base64.RawURLEncoding.EncodeToString(keyIDRaw)
	signingKey := s.hasher.Hash(base64.RawURLEncoding.EncodeToString(rawSecret))

	create := domain.SessionCreate{
		UserID:         in.UserID,
		HMACKeyID:      keyID,
		HMACSecretHash: signingKey, // same value the client receives
		ExpiresAt:      s.now().Add(s.slidingExpiry),
	}
	if in.ClientIP != "" {
		create.ClientIP = &in.ClientIP
	}
	if in.UserAgent != "" {
		create.UserAgent = &in.UserAgent
	}

	row, err := s.repo.Create(ctx, create)
	if err != nil {
		return nil, fmt.Errorf("session.Mint: persist: %w", err)
	}
	return &MintResult{Session: row, KeyID: keyID, SigningKey: signingKey}, nil
}

// SignedRequest carries the raw inputs needed to validate an HMAC-signed
// request. Body MUST be the exact bytes the signer used (the middleware
// reads it once, computes the digest, and restores r.Body before
// forwarding).
type SignedRequest struct {
	APIKey       string // X-API-Key
	Signature    string // X-Signature (hex of HMAC-SHA256)
	TimestampStr string // X-Timestamp (Unix seconds, decimal)
	Method       string
	Path         string
	Body         []byte
}

// ValidateSignedRequest authenticates a request signed under v2 per-user
// HMAC and slides the session's expiry.
//
// Returns:
//   - (*Session, nil) on success;
//   - (nil, ErrSessionInvalidated) for any auth failure the caller should
//     surface as 401 (revoked, expired, bad signature, drifted clock);
//   - (nil, wrapped non-domain error) for infra failures.
func (s *SessionService) ValidateSignedRequest(ctx context.Context, req SignedRequest) (*domain.Session, error) {
	if req.APIKey == "" || req.Signature == "" || req.TimestampStr == "" {
		return nil, fmt.Errorf("session.Validate: missing X-API-Key / X-Signature / X-Timestamp")
	}

	ts, err := strconv.ParseInt(req.TimestampStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("session.Validate: invalid timestamp: %w", err)
	}
	skew := s.now().Sub(time.Unix(ts, 0))
	if skew < -MaxTimestampDrift || skew > MaxTimestampDrift {
		return nil, domain.ErrSessionInvalidated
	}

	row, err := s.repo.FindByKeyID(ctx, req.APIKey)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrSessionInvalidated
		}
		return nil, err
	}
	if !row.Valid() {
		return nil, domain.ErrSessionInvalidated
	}

	expected := computeHMAC(row.HMACSecretHash, req.Method, req.Path, ts, req.Body)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(req.Signature)) != 1 {
		return nil, domain.ErrSessionInvalidated
	}

	if err := s.repo.Touch(ctx, row.ID, s.slidingExpiry); err != nil {
		return nil, fmt.Errorf("session.Validate: touch: %w", err)
	}
	return row, nil
}

// Logout revokes the session with reason "self-logout".
func (s *SessionService) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return s.repo.Revoke(ctx, sessionID, domain.SessionRevokeReasonSelfLogout)
}

// RevokeAllForUser is the FR-030b force-logout primitive. Returns the
// count of sessions actually transitioned.
func (s *SessionService) RevokeAllForUser(ctx context.Context, userID uuid.UUID, reason string) (int64, error) {
	return s.repo.RevokeAllForUser(ctx, userID, reason)
}

// SignRequest computes the X-Signature value a client would emit for the
// given inputs. Public so test code and integration helpers can sign
// requests without duplicating the canonical-string layout.
func SignRequest(signingKey, method, path string, ts int64, body []byte) string {
	return computeHMAC(signingKey, method, path, ts, body)
}

func computeHMAC(signingKey, method, path string, ts int64, body []byte) string {
	stringToSign := fmt.Sprintf("%s\n%s\n%d\n%s", method, path, ts, string(body))
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write([]byte(stringToSign))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomBytes(n int) ([]byte, error) {
	out := make([]byte, n)
	if _, err := rand.Read(out); err != nil {
		return nil, err
	}
	return out, nil
}
