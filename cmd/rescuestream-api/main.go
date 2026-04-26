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

	// Create services
	auditLogService := service.NewAuditLogService(auditLogRepo, service.WithAuditLogLogger(logger))

	// Create handlers
	healthHandler := handler.NewHealthHandler(pool)
	auditLogHandler := handler.NewAuditLogHandler(auditLogService, logger)

	// Create key store for HMAC auth.
	// TODO(003-multi-tenant-platform T028/T029): replace EnvKeyStore +
	// shared API_SECRET with the per-user-session HMAC store
	// (sessions table, peppered hashes). Until then this is what gates
	// every authenticated v2 endpoint that lands on this branch.
	keyStore := handler.NewEnvKeyStore(c.APISecret)
	authMiddleware := handler.NewAuthMiddleware(keyStore, logger)

	// Create and start HTTP server
	srv := server.New(c.APIPort,
		server.WithLogger(logger),
		server.WithAuthMiddleware(authMiddleware),
		server.WithHealthHandler(healthHandler),
		server.WithAuditLogHandler(auditLogHandler),
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
