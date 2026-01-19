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
	MediaMTXAPIURL    string `env:"MEDIAMTX_API_URL" envDefault:"http://localhost:9997"`
	MediaMTXPublicURL string `env:"MEDIAMTX_PUBLIC_URL" envDefault:"http://localhost:8889"`
}

func NewConfig() (*Config, error) {
	var cfg Config

	err := env.Parse(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}
