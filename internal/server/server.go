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
	healthHandler        http.Handler
	auditLogHandler      http.Handler
	superAdminHandler    *handler.SuperAdminHandler
	organizationHandler  *handler.OrganizationHandler
	teamHandler          *handler.TeamHandler
	sessionsHandler      *handler.SessionsHandler
	sessionAuthOnlyChain *handler.AuthMiddleware // identity-resolver-less variant for /sessions/logout

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

// WithSuperAdminHandler sets the super-admin handler. Routes registered
// for super-admin endpoints are wrapped in RequireSuperAdmin so callers
// without the role get a 403 forbidden upstream of the handler.
func WithSuperAdminHandler(h *handler.SuperAdminHandler) Option {
	return func(s *Server) {
		s.superAdminHandler = h
	}
}

// WithOrganizationHandler sets the /orgs handler. The handler enforces
// per-route authorization itself (super-admin vs org-admin) because the
// policy varies by route.
func WithOrganizationHandler(h *handler.OrganizationHandler) Option {
	return func(s *Server) {
		s.organizationHandler = h
	}
}

// WithTeamHandler sets the team handler. Team routes live under both
// /orgs/{org_id}/teams (collection) and /teams/{team_id} (resource);
// authorization is enforced inside each handler method.
func WithTeamHandler(h *handler.TeamHandler) Option {
	return func(s *Server) {
		s.teamHandler = h
	}
}

// WithSessionsHandler sets the /sessions handler. POST /sessions/login-
// complete is wired as a public route; POST /sessions/logout is wired
// behind the session-only auth middleware (sessionAuthOnlyChain) so
// no-org-membership users can still log out — see WithSessionAuthOnly.
func WithSessionsHandler(h *handler.SessionsHandler) Option {
	return func(s *Server) {
		s.sessionsHandler = h
	}
}

// WithSessionAuthOnly sets the auth middleware variant used by routes
// that need a valid session BUT must NOT reject no-org-membership
// callers. Construct it with NewAuthMiddleware(sessionService, nil, logger).
// Required when WithSessionsHandler is set so /sessions/logout can be
// reached by any signed-in user.
func WithSessionAuthOnly(m *handler.AuthMiddleware) Option {
	return func(s *Server) {
		s.sessionAuthOnlyChain = m
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

	// Public sign-in route (the body itself is authenticated by the
	// Google id_token verification step inside the handler).
	if s.sessionsHandler != nil {
		s.router.HandleFunc("/sessions/login-complete", s.sessionsHandler.LoginComplete).
			Methods(http.MethodPost)
	}

	// /sessions/logout requires a valid session BUT must reach
	// no-org-membership users (otherwise they can never log out).
	// Wire it on a session-only middleware chain that skips identity
	// resolution.
	if s.sessionsHandler != nil && s.sessionAuthOnlyChain != nil {
		sessionsOnly := s.router.PathPrefix("").Subrouter()
		sessionsOnly.Use(s.sessionAuthOnlyChain.Authenticate)
		sessionsOnly.HandleFunc("/sessions/logout", s.sessionsHandler.Logout).
			Methods(http.MethodPost)
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

		if s.superAdminHandler != nil {
			// Super-admin endpoints are gated by RequireSuperAdmin in
			// addition to AuthMiddleware (api-routes.md §3, FR-005).
			sa := protected.PathPrefix("/super-admins").Subrouter()
			sa.Use(handler.RequireSuperAdmin)
			sa.HandleFunc("", s.superAdminHandler.List).Methods(http.MethodGet)
			sa.HandleFunc("", s.superAdminHandler.Add).Methods(http.MethodPost)
			sa.HandleFunc("/{user_id}", s.superAdminHandler.Remove).Methods(http.MethodDelete)
		}

		if s.organizationHandler != nil {
			// /orgs authorization varies per-route (super-admin vs
			// org-admin of the target org). The handler enforces it
			// internally; we don't put RequireSuperAdmin on the
			// subrouter so org-admins can reach GET/PATCH on their own org.
			org := protected.PathPrefix("/orgs").Subrouter()
			org.HandleFunc("", s.organizationHandler.Create).Methods(http.MethodPost)
			org.HandleFunc("", s.organizationHandler.List).Methods(http.MethodGet)
			org.HandleFunc("/{id}", s.organizationHandler.Get).Methods(http.MethodGet)
			org.HandleFunc("/{id}", s.organizationHandler.Update).Methods(http.MethodPatch)
			org.HandleFunc("/{id}", s.organizationHandler.Delete).Methods(http.MethodDelete)

			if s.organizationHandler.HasAdminsService() {
				org.HandleFunc("/{id}/admins", s.organizationHandler.AddAdmin).Methods(http.MethodPost)
				org.HandleFunc("/{id}/admins/{user_id}", s.organizationHandler.RemoveAdmin).Methods(http.MethodDelete)
			}
		}

		if s.teamHandler != nil {
			// Team routes split between org-scoped collection paths and
			// resource paths. Per-route authz lives inside the handler.
			protected.HandleFunc("/orgs/{org_id}/teams", s.teamHandler.Create).Methods(http.MethodPost)
			protected.HandleFunc("/orgs/{org_id}/teams", s.teamHandler.ListByOrg).Methods(http.MethodGet)
			protected.HandleFunc("/teams/{team_id}", s.teamHandler.Get).Methods(http.MethodGet)
			protected.HandleFunc("/teams/{team_id}", s.teamHandler.Update).Methods(http.MethodPatch)
			protected.HandleFunc("/teams/{team_id}", s.teamHandler.Delete).Methods(http.MethodDelete)
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
