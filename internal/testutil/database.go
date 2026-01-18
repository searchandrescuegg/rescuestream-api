package testutil

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
)

// TestDatabase wraps a PostgreSQL test container and connection pool.
type TestDatabase struct {
	Container testcontainers.Container
	Pool      *pgxpool.Pool
	URL       string
}

// SetupTestDatabase creates a new PostgreSQL test container and returns
// a connection pool. It also runs migrations.
func SetupTestDatabase(t *testing.T) *TestDatabase {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("rescuestream_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	// Run migrations
	if err := database.RunMigrations(connStr); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Create connection pool
	pool, err := database.NewPool(ctx, connStr, database.WithTracing(false))
	if err != nil {
		t.Fatalf("failed to create connection pool: %v", err)
	}

	return &TestDatabase{
		Container: container,
		Pool:      pool,
		URL:       connStr,
	}
}

// Cleanup cleans up the test database.
func (td *TestDatabase) Cleanup(t *testing.T) {
	t.Helper()
	if td.Pool != nil {
		td.Pool.Close()
	}
	if td.Container != nil {
		if err := td.Container.Terminate(context.Background()); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}
}

// TruncateTables truncates all tables in the database.
func (td *TestDatabase) TruncateTables(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	tables := []string{"streams", "stream_keys", "broadcasters"}
	for _, table := range tables {
		_, err := td.Pool.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
		if err != nil {
			t.Fatalf("failed to truncate table %s: %v", table, err)
		}
	}
}
