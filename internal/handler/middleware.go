package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

type contextKey string

const (
	requestIDKey  contextKey = "request_id"
	apiKeyKey     contextKey = "api_key"
	auditStateKey contextKey = "audit_state"
	sessionIDKey  contextKey = "session_id"
	userIDKey     contextKey = "user_id"
)

// SessionIDFromContext returns the authenticated session id (uuid.Nil
// if no auth ran on this request).
func SessionIDFromContext(ctx context.Context) uuid.UUID {
	if id, ok := ctx.Value(sessionIDKey).(uuid.UUID); ok {
		return id
	}
	return uuid.Nil
}

// UserIDFromContext returns the authenticated user id (uuid.Nil if no
// auth ran on this request).
func UserIDFromContext(ctx context.Context) uuid.UUID {
	if id, ok := ctx.Value(userIDKey).(uuid.UUID); ok {
		return id
	}
	return uuid.Nil
}

// AuditState holds before/after state for update operations.
type AuditState struct {
	Before interface{} `json:"before,omitempty"`
	After  interface{} `json:"after,omitempty"`
}

// RequestIDFromContext returns the request ID from the context.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// APIKeyFromContext returns the authenticated API key from the context.
func APIKeyFromContext(ctx context.Context) string {
	if key, ok := ctx.Value(apiKeyKey).(string); ok {
		return key
	}
	return ""
}

// AuditStateFromContext returns the audit state from the context.
func AuditStateFromContext(ctx context.Context) *AuditState {
	if state, ok := ctx.Value(auditStateKey).(*AuditState); ok {
		return state
	}
	return nil
}

// SetAuditStateBefore sets the "before" state in context for audit logging.
// Handlers should call this before performing update operations.
func SetAuditStateBefore(ctx context.Context, before interface{}) context.Context {
	state := AuditStateFromContext(ctx)
	if state == nil {
		state = &AuditState{}
	}
	state.Before = before
	return context.WithValue(ctx, auditStateKey, state)
}

// SetAuditStateAfter sets the "after" state in context for audit logging.
// Handlers should call this after performing update operations.
func SetAuditStateAfter(ctx context.Context, after interface{}) context.Context {
	state := AuditStateFromContext(ctx)
	if state == nil {
		state = &AuditState{}
	}
	state.After = after
	return context.WithValue(ctx, auditStateKey, state)
}

// ContextWithAPIKey returns a context with the API key set. Primarily for testing.
func ContextWithAPIKey(ctx context.Context, apiKey string) context.Context {
	return context.WithValue(ctx, apiKeyKey, apiKey)
}

// ContextWithRequestID returns a context with the request ID set. Primarily for testing.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDMiddleware adds a unique request ID to each request.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware logs request details.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)
			requestID := RequestIDFromContext(r.Context())

			logger.Info("request completed",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.statusCode),
				slog.Duration("duration", duration),
				slog.String("request_id", requestID),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

// AuthMiddleware authenticates v2 HMAC-signed requests against the
// server-side session store. Replaces the v1 shared API_SECRET path.
type AuthMiddleware struct {
	sessions *service.SessionService
	logger   *slog.Logger
}

// NewAuthMiddleware constructs a session-backed authentication middleware.
func NewAuthMiddleware(sessions *service.SessionService, logger *slog.Logger) *AuthMiddleware {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuthMiddleware{sessions: sessions, logger: logger}
}

// Authenticate validates X-API-Key + X-Signature + X-Timestamp on every
// request. On success the session id, user id, and X-API-Key (legacy
// `apiKey` context value, for the audit middleware's actor field) are
// attached to the request context.
//
// Auth failure modes are surfaced as RFC 9457 problems:
//
//   - missing/blank headers       → /errors/unauthorized (401)
//   - body-read failure           → /errors/internal-error (500)
//   - any session-validation
//     failure (revoked, expired,
//     unknown key, bad signature,
//     drifted clock)              → /problems/session-invalidated (401)
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		signature := r.Header.Get("X-Signature")
		timestampStr := r.Header.Get("X-Timestamp")

		if apiKey == "" || signature == "" || timestampStr == "" {
			m.logger.Warn("missing authentication headers",
				slog.String("remote_addr", r.RemoteAddr),
				slog.Bool("has_api_key", apiKey != ""),
				slog.Bool("has_signature", signature != ""),
				slog.Bool("has_timestamp", timestampStr != ""),
			)
			WriteError(w, r, ErrUnauthorized("Missing authentication headers (X-API-Key, X-Signature, X-Timestamp)"))
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			m.logger.Error("failed to read request body", slog.String("error", err.Error()))
			WriteError(w, r, ErrInternalServer("Failed to read request body"))
			return
		}
		// Restore body for downstream handlers.
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		row, err := m.sessions.ValidateSignedRequest(r.Context(), service.SignedRequest{
			APIKey:       apiKey,
			Signature:    signature,
			TimestampStr: timestampStr,
			Method:       r.Method,
			Path:         r.URL.Path,
			Body:         bodyBytes,
		})
		if err != nil {
			if errors.Is(err, domain.ErrSessionInvalidated) {
				m.logger.Warn("session invalidated",
					slog.String("api_key", apiKey),
					slog.String("remote_addr", r.RemoteAddr),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
				)
				WriteError(w, r, MapDomainError(domain.ErrSessionInvalidated))
				return
			}
			// Anything else (missing-headers leak-through, infra
			// failure, etc.) is a client mistake or operator issue —
			// surface as 401 rather than expose internals.
			m.logger.Error("session validation error",
				slog.String("error", err.Error()),
				slog.String("api_key", apiKey),
			)
			WriteError(w, r, ErrUnauthorized("Authentication failed"))
			return
		}

		m.logger.Debug("request authenticated",
			slog.String("session_id", row.ID.String()),
			slog.String("user_id", row.UserID.String()),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)

		ctx := context.WithValue(r.Context(), sessionIDKey, row.ID)
		ctx = context.WithValue(ctx, userIDKey, row.UserID)
		// apiKeyKey is preserved as the X-API-Key value so the existing
		// audit middleware's actor field continues to identify the
		// session uniquely. The v2 audit log vocabulary rewrite (T032)
		// will switch the actor source to the user_id.
		ctx = context.WithValue(ctx, apiKeyKey, apiKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// getClientIP extracts the client IP address from the request.
// It checks X-Forwarded-For and X-Real-IP headers first (for proxy scenarios),
// then falls back to RemoteAddr.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (load balancer/proxy)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take first IP in comma-separated list
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header (common alternative)
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// sanitizePath returns the path unchanged. Reserved as a sanitization
// hook for future routes that may carry sensitive data; currently a no-op
// since the v1 /auth path that needed redacting was retired.
func sanitizePath(path string) string {
	return path
}

// extractResourceInfo parses resource type and ID from a request path of
// the shape `/<resource>/<uuid>/...`. Returns ("", nil) when no recognizable
// resource segment is present. The first segment is taken verbatim (no
// whitelist) — the audit-log resource_type vocabulary is enforced at the
// emission site, not at the path-parser level.
func extractResourceInfo(path string) (string, *uuid.UUID) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		return "", nil
	}
	resourceType := parts[0]
	if len(parts) >= 2 {
		if id, err := uuid.Parse(parts[1]); err == nil {
			return resourceType, &id
		}
	}
	return resourceType, nil
}

// methodToAction maps HTTP methods to audit action types.
func methodToAction(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodPatch, http.MethodPut:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// statusToOutcome maps HTTP status codes to audit outcomes.
func statusToOutcome(status int) string {
	if status >= 200 && status < 400 {
		return "success"
	}
	return "failure"
}

// AuditMiddleware logs all mutating requests to the audit log.
func AuditMiddleware(auditService *service.AuditLogService, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip non-mutating methods
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Skip audit endpoints to prevent recursion
			if r.URL.Path == "/audit-events" || r.URL.Path == "/audit-logs" {
				next.ServeHTTP(w, r)
				return
			}

			// Capture response status
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			// Build and log audit entry
			ctx := r.Context()
			apiKey := APIKeyFromContext(ctx)
			requestID := RequestIDFromContext(ctx)

			entry := &domain.AuditLogEntry{
				Actor:         apiKey,
				Action:        methodToAction(r.Method),
				RequestMethod: r.Method,
				RequestPath:   sanitizePath(r.URL.Path),
				IPAddress:     getClientIP(r),
				Outcome:       statusToOutcome(wrapped.statusCode),
				Metadata:      make(map[string]interface{}),
			}

			if requestID != "" {
				entry.RequestID = &requestID
			}

			// Extract resource info from path
			resourceType, resourceID := extractResourceInfo(r.URL.Path)
			if resourceType != "" {
				entry.ResourceType = &resourceType
			}
			if resourceID != nil {
				entry.ResourceID = resourceID
			}

			if wrapped.statusCode >= 400 {
				reason := fmt.Sprintf("HTTP %d", wrapped.statusCode)
				entry.FailureReason = &reason
			}

			// Capture before/after state for update operations
			if auditState := AuditStateFromContext(ctx); auditState != nil {
				if auditState.Before != nil {
					entry.Metadata["before"] = auditState.Before
				}
				if auditState.After != nil {
					entry.Metadata["after"] = auditState.After
				}
			}

			if err := auditService.CreateEntry(ctx, entry); err != nil {
				logger.Error("audit logging failed",
					slog.String("error", err.Error()),
					slog.String("path", r.URL.Path),
				)
			}
		})
	}
}
