package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"alpineworks.io/ootel"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"

	"github.com/searchandrescuegg/rescuestream-api/internal/config"
	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/handler"
	"github.com/searchandrescuegg/rescuestream-api/internal/logging"
	"github.com/searchandrescuegg/rescuestream-api/internal/pepper"
	"github.com/searchandrescuegg/rescuestream-api/internal/server"
	"github.com/searchandrescuegg/rescuestream-api/internal/service"
)

func main() {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "error"
	}

	slogLevel, err := logging.LogLevelToSlogLevel(logLevel)
	if err != nil {
		log.Fatalf("could not convert log level: %s", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogLevel,
	}))
	slog.SetDefault(logger)

	c, err := config.NewConfig()
	if err != nil {
		slog.Error("could not create config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx := context.Background()

	// Initialize OpenTelemetry
	exporterType := ootel.ExporterTypePrometheus
	if c.Local {
		exporterType = ootel.ExporterTypeOTLPGRPC
	}

	ootelClient := ootel.NewOotelClient(
		ootel.WithMetricConfig(
			ootel.NewMetricConfig(
				c.MetricsEnabled,
				exporterType,
				c.MetricsPort,
			),
		),
		ootel.WithTraceConfig(
			ootel.NewTraceConfig(
				c.TracingEnabled,
				c.TracingSampleRate,
				c.TracingService,
				c.TracingVersion,
			),
		),
	)

	shutdown, err := ootelClient.Init(ctx)
	if err != nil {
		slog.Error("could not create ootel client", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		if shutdownErr := shutdown(ctx); shutdownErr != nil {
			slog.Error("failed to shutdown telemetry", slog.String("error", shutdownErr.Error()))
		}
	}()

	err = runtime.Start(runtime.WithMinimumReadMemStatsInterval(5 * time.Second))
	if err != nil {
		slog.Error("could not create runtime metrics", slog.String("error", err.Error()))
		os.Exit(1)
	}

	err = host.Start()
	if err != nil {
		slog.Error("could not create host metrics", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Migrations are operator-initiated via `just migrate-prod` (or
	// `just migrate-local` in dev) — see the 2026-04-21 spec clarification
	// (Q3) and the runbook in tasks.md T115a. The API container MUST NOT
	// run migrations on boot, so a failed migration cannot crash-loop the
	// service.

	// Create database connection pool
	pool, err := database.NewPool(ctx, c.DatabaseURL,
		database.WithTracing(c.TracingEnabled),
	)
	if err != nil {
		slog.Error("failed to create database pool", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	// Create repositories (v1 broadcaster/streamkey/stream repos retired
	// at the v2 cutover; their v2 replacements land with the device + room
	// + session work in subsequent commits).
	auditLogRepo := database.NewAuditLogRepo(pool)
	sessionRepo := database.NewSessionRepo(pool)
	superAdminRepo := database.NewSuperAdminRepo(pool)
	membershipRepo := database.NewMembershipRepo(pool)
	orgRepo := database.NewOrganizationRepo(pool)

	// Build the peppered HMAC hasher used for session secret hashing.
	// SESSION_SECRET_PEPPER is required at boot; the hasher itself
	// validates the minimum length.
	sessionPepper, err := pepper.New(c.SessionSecretPepper)
	if err != nil {
		slog.Error("invalid SESSION_SECRET_PEPPER", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Create services
	auditLogService := service.NewAuditLogService(auditLogRepo, service.WithAuditLogLogger(logger))
	sessionService := service.NewSessionService(sessionRepo, sessionPepper,
		service.WithSlidingExpiry(time.Duration(c.SessionExpiryDays)*24*time.Hour),
	)
	identityResolver := service.NewIdentityResolver(superAdminRepo, membershipRepo, orgRepo)
	userRepo := database.NewUserRepo(pool)
	superAdminService := service.NewSuperAdminService(pool, superAdminRepo, userRepo)

	// Create handlers
	healthHandler := handler.NewHealthHandler(pool)
	auditLogHandler := handler.NewAuditLogHandler(auditLogService, logger)
	superAdminHandler := handler.NewSuperAdminHandler(superAdminService, logger)

	// AuthMiddleware authenticates every protected request against the
	// server-side session store (research §3) and resolves the caller's
	// tenancy identity (super-admin / org-admin / member). The shared
	// API_SECRET path from v1 is gone — sessions are minted out of the
	// OAuth callback handler (lands with US2 sign-in flow).
	authMiddleware := handler.NewAuthMiddleware(sessionService, identityResolver, logger)

	// Create and start HTTP server
	srv := server.New(c.APIPort,
		server.WithLogger(logger),
		server.WithAuthMiddleware(authMiddleware),
		server.WithHealthHandler(healthHandler),
		server.WithAuditLogHandler(auditLogHandler),
		server.WithSuperAdminHandler(superAdminHandler),
		server.WithAuditService(auditLogService),
	)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil {
			slog.Error("server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	slog.Info("server started", slog.Int("port", c.APIPort))

	<-sigCh
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", slog.String("error", err.Error()))
	}

	slog.Info("shutdown complete")
}
