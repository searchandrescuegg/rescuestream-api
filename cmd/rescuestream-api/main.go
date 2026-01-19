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

	// Run database migrations
	slog.Info("running database migrations")
	if migrationErr := database.RunMigrations(c.DatabaseURL); migrationErr != nil {
		slog.Error("failed to run migrations", slog.String("error", migrationErr.Error()))
		os.Exit(1)
	}
	slog.Info("database migrations completed")

	// Create database connection pool
	pool, err := database.NewPool(ctx, c.DatabaseURL,
		database.WithTracing(c.TracingEnabled),
	)
	if err != nil {
		slog.Error("failed to create database pool", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	// Create repositories
	broadcasterRepo := database.NewBroadcasterRepo(pool)
	streamKeyRepo := database.NewStreamKeyRepo(pool)
	streamRepo := database.NewStreamRepo(pool)

	// Create MediaMTX client
	mediaMTXClient, err := service.NewMediaMTXClient(
		c.MediaMTXAPIURL,
		c.MediaMTXPublicURL,
		service.WithMediaMTXLogger(logger),
	)
	if err != nil {
		slog.Error("failed to create MediaMTX client", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Create services
	authService := service.NewAuthService(pool, streamKeyRepo, streamRepo, service.WithAuthLogger(logger))
	streamService := service.NewStreamService(streamRepo, mediaMTXClient, service.WithStreamLogger(logger))
	streamKeyService := service.NewStreamKeyService(streamKeyRepo, streamRepo, mediaMTXClient, service.WithStreamKeyLogger(logger))
	broadcasterService := service.NewBroadcasterService(broadcasterRepo, service.WithBroadcasterLogger(logger))

	// Create handlers
	authHandler := handler.NewAuthHandler(authService, logger)
	webhookHandler := handler.NewWebhookHandler(streamRepo, streamKeyRepo, logger)
	streamHandler := handler.NewStreamHandler(streamService, logger)
	streamKeyHandler := handler.NewStreamKeyHandler(streamKeyService, logger)
	broadcasterHandler := handler.NewBroadcasterHandler(broadcasterService, logger)
	healthHandler := handler.NewHealthHandler(pool)

	// Create key store for HMAC auth
	keyStore := handler.NewEnvKeyStore(c.APISecret)
	authMiddleware := handler.NewAuthMiddleware(keyStore, logger)

	// Create and start HTTP server
	srv := server.New(c.APIPort,
		server.WithLogger(logger),
		server.WithAuthMiddleware(authMiddleware),
		server.WithAuthHandler(authHandler),
		server.WithWebhookHandler(webhookHandler),
		server.WithStreamHandler(streamHandler),
		server.WithStreamKeyHandler(streamKeyHandler),
		server.WithBroadcasterHandler(broadcasterHandler),
		server.WithHealthHandler(healthHandler),
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
