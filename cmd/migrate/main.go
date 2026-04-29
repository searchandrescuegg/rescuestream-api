package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: migrate <up|down|version>")
		os.Exit(1)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://postgres:postgres@localhost:5432/rescuestream?sslmode=disable"
	}

	command := os.Args[1]

	switch command {
	case "up":
		if err := database.RunMigrations(databaseURL); err != nil {
			log.Fatalf("Failed to run migrations: %v", err)
		}
		fmt.Println("Migrations applied successfully")

		// After v2 schema is in place (migration 000003+), bootstrap super_admins
		// from SUPER_ADMIN_EMAILS per FR-005. No-op if the env var is unset.
		if emails := parseSuperAdminEmails(os.Getenv("SUPER_ADMIN_EMAILS")); len(emails) > 0 {
			n, err := seedSuperAdmins(databaseURL, emails)
			if err != nil {
				log.Fatalf("Failed to seed super-admins: %v", err)
			}
			fmt.Printf("Seeded %d super-admin(s) from SUPER_ADMIN_EMAILS\n", n)
		}

	case "down":
		if err := database.RollbackMigrations(databaseURL); err != nil {
			log.Fatalf("Failed to rollback migration: %v", err)
		}
		fmt.Println("Migration rolled back successfully")

	case "version":
		version, dirty, err := database.MigrationVersion(databaseURL)
		if err != nil {
			log.Fatalf("Failed to get migration version: %v", err)
		}
		if dirty {
			fmt.Printf("Version: %d (dirty)\n", version)
		} else {
			fmt.Printf("Version: %d\n", version)
		}

	default:
		fmt.Printf("Unknown command: %s\n", command)
		fmt.Println("Usage: migrate <up|down|version>")
		os.Exit(1)
	}
}

// parseSuperAdminEmails splits a comma-separated env value into a deduped,
// lowercased, trimmed list of non-empty emails.
func parseSuperAdminEmails(raw string) []string {
	if raw == "" {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for part := range strings.SplitSeq(raw, ",") {
		email := strings.ToLower(strings.TrimSpace(part))
		if email == "" {
			continue
		}
		if _, dup := seen[email]; dup {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	return out
}

// seedSuperAdmins upserts a users row per email and ensures a matching
// super_admins row with seeded_from_env=true. Idempotent: re-running with the
// same input is a no-op.
func seedSuperAdmins(databaseURL string, emails []string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return 0, fmt.Errorf("connect: %w", err)
	}
	defer func() {
		if cerr := conn.Close(ctx); cerr != nil {
			log.Printf("seedSuperAdmins: closing connection: %v", cerr)
		}
	}()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	// Rollback is a no-op after a successful Commit; intentionally ignore
	// pgx.ErrTxClosed in that case.
	defer func() {
		if rerr := tx.Rollback(ctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			log.Printf("seedSuperAdmins: rolling back tx: %v", rerr)
		}
	}()

	added := 0
	for _, email := range emails {
		var userID uuid.UUID
		err := tx.QueryRow(ctx, `
            INSERT INTO users (id, email)
            VALUES ($1, $2)
            ON CONFLICT (email) DO UPDATE SET updated_at = NOW()
            RETURNING id
        `, uuid.New(), email).Scan(&userID)
		if err != nil {
			return added, fmt.Errorf("upsert user %s: %w", email, err)
		}

		tag, err := tx.Exec(ctx, `
            INSERT INTO super_admins (id, user_id, seeded_from_env)
            VALUES ($1, $2, TRUE)
            ON CONFLICT (user_id) DO NOTHING
        `, uuid.New(), userID)
		if err != nil {
			return added, fmt.Errorf("upsert super_admin for %s: %w", email, err)
		}
		if tag.RowsAffected() == 1 {
			added++
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return added, fmt.Errorf("commit: %w", err)
	}
	return added, nil
}
