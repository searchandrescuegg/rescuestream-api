package database

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Option is a functional option for configuring the database client.
type Option func(*options)

type options struct {
	minConns          int32
	maxConns          int32
	maxConnLifetime   time.Duration
	maxConnIdleTime   time.Duration
	healthCheckPeriod time.Duration
	enableTracing     bool
}

func defaultOptions() *options {
	return &options{
		minConns:          5,
		maxConns:          25,
		maxConnLifetime:   time.Hour,
		maxConnIdleTime:   30 * time.Minute,
		healthCheckPeriod: 30 * time.Second,
		enableTracing:     true,
	}
}

// WithMinConns sets the minimum number of connections in the pool.
func WithMinConns(n int32) Option {
	return func(o *options) {
		o.minConns = n
	}
}

// WithMaxConns sets the maximum number of connections in the pool.
func WithMaxConns(n int32) Option {
	return func(o *options) {
		o.maxConns = n
	}
}

// WithMaxConnLifetime sets the maximum lifetime of a connection.
func WithMaxConnLifetime(d time.Duration) Option {
	return func(o *options) {
		o.maxConnLifetime = d
	}
}

// WithMaxConnIdleTime sets the maximum idle time of a connection.
func WithMaxConnIdleTime(d time.Duration) Option {
	return func(o *options) {
		o.maxConnIdleTime = d
	}
}

// WithHealthCheckPeriod sets the health check period for connections.
func WithHealthCheckPeriod(d time.Duration) Option {
	return func(o *options) {
		o.healthCheckPeriod = d
	}
}

// WithTracing enables or disables OpenTelemetry tracing.
func WithTracing(enabled bool) Option {
	return func(o *options) {
		o.enableTracing = enabled
	}
}

// NewPool creates a new pgxpool with the given connection string and options.
func NewPool(ctx context.Context, connString string, opts ...Option) (*pgxpool.Pool, error) {
	cfg := defaultOptions()
	for _, opt := range opts {
		opt(cfg)
	}

	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	poolConfig.MinConns = cfg.minConns
	poolConfig.MaxConns = cfg.maxConns
	poolConfig.MaxConnLifetime = cfg.maxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.maxConnIdleTime
	poolConfig.HealthCheckPeriod = cfg.healthCheckPeriod

	if cfg.enableTracing {
		poolConfig.ConnConfig.Tracer = otelpgx.NewTracer()
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}
