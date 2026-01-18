package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/searchandrescuegg/rescuestream-api/internal/handler"
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
	authHandler        http.Handler
	webhookHandler     http.Handler
	streamHandler      http.Handler
	streamKeyHandler   http.Handler
	broadcasterHandler http.Handler
	healthHandler      http.Handler
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

// WithAuthHandler sets the auth handler.
func WithAuthHandler(h http.Handler) Option {
	return func(s *Server) {
		s.authHandler = h
	}
}

// WithWebhookHandler sets the webhook handler.
func WithWebhookHandler(h http.Handler) Option {
	return func(s *Server) {
		s.webhookHandler = h
	}
}

// WithStreamHandler sets the stream handler.
func WithStreamHandler(h http.Handler) Option {
	return func(s *Server) {
		s.streamHandler = h
	}
}

// WithStreamKeyHandler sets the stream key handler.
func WithStreamKeyHandler(h http.Handler) Option {
	return func(s *Server) {
		s.streamKeyHandler = h
	}
}

// WithBroadcasterHandler sets the broadcaster handler.
func WithBroadcasterHandler(h http.Handler) Option {
	return func(s *Server) {
		s.broadcasterHandler = h
	}
}

// WithHealthHandler sets the health handler.
func WithHealthHandler(h http.Handler) Option {
	return func(s *Server) {
		s.healthHandler = h
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

	// Public routes (no auth required) - for MediaMTX
	if s.authHandler != nil {
		s.router.Handle("/auth", s.authHandler).Methods(http.MethodPost)
	}

	if s.webhookHandler != nil {
		s.router.Handle("/webhook/ready", s.webhookHandler).Methods(http.MethodPost)
		s.router.Handle("/webhook/not-ready", s.webhookHandler).Methods(http.MethodPost)
	}

	// Health check (no auth required)
	if s.healthHandler != nil {
		s.router.Handle("/health", s.healthHandler).Methods(http.MethodGet)
	}

	// Protected routes (require auth)
	if s.authMiddleware != nil {
		protected := s.router.PathPrefix("").Subrouter()
		protected.Use(s.authMiddleware.Authenticate)

		if s.streamHandler != nil {
			protected.Handle("/streams", s.streamHandler).Methods(http.MethodGet)
			protected.Handle("/streams/{id}", s.streamHandler).Methods(http.MethodGet)
		}

		if s.streamKeyHandler != nil {
			protected.Handle("/stream-keys", s.streamKeyHandler).Methods(http.MethodGet, http.MethodPost)
			protected.Handle("/stream-keys/{id}", s.streamKeyHandler).Methods(http.MethodGet, http.MethodDelete)
		}

		if s.broadcasterHandler != nil {
			protected.Handle("/broadcasters", s.broadcasterHandler).Methods(http.MethodGet, http.MethodPost)
			protected.Handle("/broadcasters/{id}", s.broadcasterHandler).Methods(http.MethodGet, http.MethodPatch, http.MethodDelete)
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
