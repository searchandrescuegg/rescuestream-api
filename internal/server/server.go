package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/searchandrescuegg/rescuestream-api/internal/handler"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

// Option is a functional option for configuring the server.
type Option func(*Server)

// Server represents the HTTP server.
type Server struct {
	router         *mux.Router
	server         *http.Server
	logger         *slog.Logger
	authMiddleware *handler.AuthMiddleware

	// Handler dependencies (set via options)
	healthHandler   http.Handler
	auditLogHandler http.Handler

	// Service dependencies for middleware
	auditService *service.AuditLogService
}

// WithLogger sets the logger for the server.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Server) {
		s.logger = logger
	}
}

// WithAuthMiddleware sets the authentication middleware.
func WithAuthMiddleware(m *handler.AuthMiddleware) Option {
	return func(s *Server) {
		s.authMiddleware = m
	}
}

// WithHealthHandler sets the health handler.
func WithHealthHandler(h http.Handler) Option {
	return func(s *Server) {
		s.healthHandler = h
	}
}

// WithAuditLogHandler sets the audit log handler.
func WithAuditLogHandler(h http.Handler) Option {
	return func(s *Server) {
		s.auditLogHandler = h
	}
}

// WithAuditService sets the audit log service for middleware.
func WithAuditService(svc *service.AuditLogService) Option {
	return func(s *Server) {
		s.auditService = svc
	}
}

// New creates a new server with the given options.
func New(port int, opts ...Option) *Server {
	s := &Server{
		router: mux.NewRouter(),
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      s.router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.setupRoutes()
	return s
}

// setupRoutes configures all HTTP routes.
func (s *Server) setupRoutes() {
	// Apply global middleware
	s.router.Use(handler.RequestIDMiddleware)
	s.router.Use(handler.LoggingMiddleware(s.logger))

	// Health check (no auth required)
	if s.healthHandler != nil {
		s.router.Handle("/health", s.healthHandler).Methods(http.MethodGet)
	}

	// Protected routes (require auth)
	if s.authMiddleware != nil {
		protected := s.router.PathPrefix("").Subrouter()
		protected.Use(s.authMiddleware.Authenticate)

		// Add audit middleware if audit service is configured
		if s.auditService != nil {
			protected.Use(handler.AuditMiddleware(s.auditService, s.logger))
		}

		if s.auditLogHandler != nil {
			protected.Handle("/audit-logs", s.auditLogHandler).Methods(http.MethodGet)
			protected.Handle("/audit-events", s.auditLogHandler).Methods(http.MethodPost)
		}
	}
}

// Router returns the underlying mux router for testing.
func (s *Server) Router() *mux.Router {
	return s.router
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	s.logger.Info("starting HTTP server", slog.String("addr", s.server.Addr))
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("failed to start server: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down HTTP server")
	return s.server.Shutdown(ctx)
}
