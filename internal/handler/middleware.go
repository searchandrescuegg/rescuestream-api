package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"alpineworks.io/rfc9457"
	"github.com/google/uuid"
)

const (
	// MaxTimestampDrift is the maximum allowed time difference for request timestamps (5 minutes)
	MaxTimestampDrift = 5 * time.Minute
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	apiKeyKey    contextKey = "api_key"
)

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

// KeyStore provides access to API keys and their secrets
type KeyStore interface {
	GetSecret(apiKey string) (string, error)
}

// AuthMiddleware provides HMAC authentication middleware
type AuthMiddleware struct {
	keyStore KeyStore
	logger   *slog.Logger
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(keyStore KeyStore, logger *slog.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		keyStore: keyStore,
		logger:   logger,
	}
}

// Authenticate wraps an HTTP handler with authentication
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract headers
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
			rfc9457.NewRFC9457(
				rfc9457.WithStatus(http.StatusUnauthorized),
				rfc9457.WithTitle("Unauthorized"),
				rfc9457.WithDetail("Missing authentication headers (X-API-Key, X-Signature, X-Timestamp)"),
				rfc9457.WithInstance(r.URL.Path),
			).ServeHTTP(w, r)
			return
		}

		// Parse timestamp
		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			m.logger.Warn("invalid timestamp format",
				slog.String("timestamp", timestampStr),
				slog.String("error", err.Error()),
			)
			rfc9457.NewRFC9457(
				rfc9457.WithStatus(http.StatusUnauthorized),
				rfc9457.WithTitle("Unauthorized"),
				rfc9457.WithDetail("Invalid timestamp format"),
				rfc9457.WithInstance(r.URL.Path),
			).ServeHTTP(w, r)
			return
		}

		// Check timestamp drift (prevent replay attacks)
		requestTime := time.Unix(timestamp, 0)
		timeDiff := time.Since(requestTime)
		if timeDiff < -MaxTimestampDrift || timeDiff > MaxTimestampDrift {
			m.logger.Warn("timestamp out of acceptable range",
				slog.String("api_key", apiKey),
				slog.Time("request_time", requestTime),
				slog.Duration("time_diff", timeDiff),
			)
			rfc9457.NewRFC9457(
				rfc9457.WithStatus(http.StatusUnauthorized),
				rfc9457.WithTitle("Unauthorized"),
				rfc9457.WithDetail("Request timestamp is too old or in the future"),
				rfc9457.WithInstance(r.URL.Path),
			).ServeHTTP(w, r)
			return
		}

		// Get secret for this API key
		secret, err := m.keyStore.GetSecret(apiKey)
		if err != nil {
			m.logger.Warn("unknown API key",
				slog.String("api_key", apiKey),
				slog.String("remote_addr", r.RemoteAddr),
			)
			rfc9457.NewRFC9457(
				rfc9457.WithStatus(http.StatusUnauthorized),
				rfc9457.WithTitle("Unauthorized"),
				rfc9457.WithDetail("Invalid API key"),
				rfc9457.WithInstance(r.URL.Path),
			).ServeHTTP(w, r)
			return
		}

		// Read body to compute signature
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			m.logger.Error("failed to read request body",
				slog.String("error", err.Error()),
			)
			rfc9457.NewRFC9457(
				rfc9457.WithStatus(http.StatusInternalServerError),
				rfc9457.WithTitle("Internal Server Error"),
				rfc9457.WithDetail("Failed to read request body"),
				rfc9457.WithInstance(r.URL.Path),
			).ServeHTTP(w, r)
			return
		}
		// Restore body for downstream handlers
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		// Compute expected signature
		stringToSign := fmt.Sprintf("%s\n%s\n%d\n%s",
			r.Method,
			r.URL.Path,
			timestamp,
			string(bodyBytes),
		)

		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte(stringToSign))
		expectedSignature := hex.EncodeToString(h.Sum(nil))

		// Compare signatures using constant-time comparison
		if subtle.ConstantTimeCompare([]byte(signature), []byte(expectedSignature)) != 1 {
			m.logger.Warn("signature verification failed",
				slog.String("api_key", apiKey),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			rfc9457.NewRFC9457(
				rfc9457.WithStatus(http.StatusUnauthorized),
				rfc9457.WithTitle("Unauthorized"),
				rfc9457.WithDetail("Invalid signature"),
				rfc9457.WithInstance(r.URL.Path),
			).ServeHTTP(w, r)
			return
		}

		// Authentication successful - add API key to context
		m.logger.Debug("request authenticated successfully",
			slog.String("api_key", apiKey),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)

		ctx := context.WithValue(r.Context(), apiKeyKey, apiKey)
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
