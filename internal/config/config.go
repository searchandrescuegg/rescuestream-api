package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	// Logging
	LogLevel string `env:"LOG_LEVEL" envDefault:"error"`

	// Metrics
	MetricsEnabled bool `env:"METRICS_ENABLED" envDefault:"true"`
	MetricsPort    int  `env:"METRICS_PORT" envDefault:"8081"`

	// Environment
	Local bool `env:"LOCAL" envDefault:"false"`

	// Tracing
	TracingEnabled    bool    `env:"TRACING_ENABLED" envDefault:"false"`
	TracingSampleRate float64 `env:"TRACING_SAMPLERATE" envDefault:"0.01"`
	TracingService    string  `env:"TRACING_SERVICE" envDefault:"rescuestream-api"`
	TracingVersion    string  `env:"TRACING_VERSION"`

	// Database
	DatabaseURL string `env:"DATABASE_URL" envDefault:"postgres://postgres:postgres@localhost:5432/rescuestream?sslmode=disable"`

	// API Server
	APIPort   int    `env:"API_PORT" envDefault:"8080"`
	APISecret string `env:"API_SECRET,required"`

	// MediaMTX Integration
	MediaMTXAPIURL        string `env:"MEDIAMTX_API_URL" envDefault:"http://localhost:9997"`
	MediaMTXPublicURL     string `env:"MEDIAMTX_PUBLIC_URL" envDefault:"http://localhost:8889"`
	MediaMTXWebhookSecret string `env:"MEDIAMTX_WEBHOOK_SECRET"`

	// Platform bootstrap (multi-tenant v2)
	SuperAdminEmails []string `env:"SUPER_ADMIN_EMAILS" envSeparator:","`

	// Secrets (peppers): hex or base64 strings, at least 32 bytes of entropy.
	// DevicePeppperPrev is set only during a rotation window.
	DeviceKeyPepper     string `env:"DEVICE_KEY_PEPPER"`
	DeviceKeyPepperPrev string `env:"DEVICE_KEY_PEPPER_PREV"`
	SessionSecretPepper string `env:"SESSION_SECRET_PEPPER"`

	// Sessions
	SessionExpiryDays int `env:"SESSION_EXPIRY_DAYS" envDefault:"30"`

	// SSE push channel
	SSEMaxConnsPerProcess int `env:"SSE_MAX_CONNS_PER_PROCESS" envDefault:"1000"`

	// Tailscale (embedded via tsnet). Disabled in local dev.
	TailscaleEnabled bool   `env:"TAILSCALE_ENABLED" envDefault:"false"`
	TailscaleAuthKey string `env:"TAILSCALE_AUTHKEY"`
}

func NewConfig() (*Config, error) {
	var cfg Config

	err := env.Parse(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}
