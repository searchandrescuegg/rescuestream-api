package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
)

// TestV2Migrations_endToEnd validates the v1 → v2 cutover migrations
// (000003-000005) by:
//
//  1. Bringing the schema up to v1 + audit_logs (migration 000002).
//  2. Seeding fixture audit_logs rows with email-shaped actor strings.
//  3. Applying the v2 cutover migrations (000003-000005).
//  4. Asserting: default org + placeholder user exist, audit_logs were
//     backfilled, broadcasters/stream_keys are gone, NOT NULL constraints
//     are in force, and the polymorphic CHECK trigger rejects bad rules.
func TestV2Migrations_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

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
	require.NoError(t, err, "start postgres container")
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// 1. Migrate to v1 audit_logs (000002) only.
	require.NoError(t, database.MigrateTo(connStr, 2), "migrate to v1+audit")

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	// 2. Seed fixture audit_logs: a real-looking email actor and a non-email actor.
	for _, row := range []struct {
		actor  string
		action string
	}{
		{"alice@example.org", "broadcaster.create"},
		{"alice@example.org", "stream_key.rotate"}, // dup actor — backfill must dedupe
		{"system", "service.start"},                // non-email actor — must NOT produce a user row
	} {
		_, err := pool.Exec(ctx, `
            INSERT INTO audit_logs (id, actor, action, request_method, request_path, ip_address)
            VALUES ($1, $2, $3, 'POST', '/v1/test', '127.0.0.1')
        `, uuid.New(), row.actor, row.action)
		require.NoError(t, err, "seed audit_logs")
	}

	// 3. Apply v2 migrations (000003-000005).
	require.NoError(t, database.MigrateTo(connStr, 5), "migrate to v2")

	// --- 4. Assertions ----------------------------------------------------

	t.Run("default org + placeholder user seeded", func(t *testing.T) {
		var (
			orgID, placeholderUserID uuid.UUID
			orgSlug, orgStatus       string
			placeholderEmail         string
		)
		err := pool.QueryRow(ctx, `
            SELECT id, slug, status FROM organizations WHERE slug = 'default'
        `).Scan(&orgID, &orgSlug, &orgStatus)
		require.NoError(t, err)
		assert.Equal(t, "default", orgSlug)
		assert.Equal(t, "active", orgStatus)
		assert.Equal(t, "00000000-0000-4000-8000-000000000002", orgID.String())

		err = pool.QueryRow(ctx, `
            SELECT id, email FROM users WHERE email = 'platform@rescue.stream'
        `).Scan(&placeholderUserID, &placeholderEmail)
		require.NoError(t, err)
		assert.Equal(t, "00000000-0000-4000-8000-000000000001", placeholderUserID.String())
	})

	t.Run("audit-actor users were backfilled and deduped", func(t *testing.T) {
		// alice@example.org should be a single users row despite appearing twice in audit_logs.
		var n int
		err := pool.QueryRow(ctx, `
            SELECT COUNT(*) FROM users WHERE email = 'alice@example.org'
        `).Scan(&n)
		require.NoError(t, err)
		assert.Equal(t, 1, n, "expected exactly one users row for the duplicated email actor")

		// "system" doesn't look like an email, so no users row should have been created for it.
		err = pool.QueryRow(ctx, `
            SELECT COUNT(*) FROM users WHERE email = 'system'
        `).Scan(&n)
		require.NoError(t, err)
		assert.Equal(t, 0, n, "non-email actors must not produce users rows")
	})

	t.Run("audit_logs backfilled with org_id and resolved actor_user_id", func(t *testing.T) {
		// Email-actor rows: organization_id set to default, actor_user_id resolved to alice.
		var (
			orgID  uuid.UUID
			userID *uuid.UUID
			n      int
		)
		err := pool.QueryRow(ctx, `
            SELECT COUNT(*) FROM audit_logs
            WHERE actor = 'alice@example.org'
              AND organization_id = '00000000-0000-4000-8000-000000000002'
              AND actor_user_id = (SELECT id FROM users WHERE email = 'alice@example.org')
        `).Scan(&n)
		require.NoError(t, err)
		assert.Equal(t, 2, n, "both alice@example.org rows must be fully backfilled")

		// Non-email actor: org_id set, actor_user_id NULL (unresolvable).
		err = pool.QueryRow(ctx, `
            SELECT organization_id, actor_user_id FROM audit_logs
            WHERE actor = 'system'
        `).Scan(&orgID, &userID)
		require.NoError(t, err)
		assert.Equal(t, "00000000-0000-4000-8000-000000000002", orgID.String())
		assert.Nil(t, userID, "non-email actor must leave actor_user_id NULL")
	})

	t.Run("v1 tables broadcasters and stream_keys are gone", func(t *testing.T) {
		for _, table := range []string{"broadcasters", "stream_keys"} {
			var exists bool
			err := pool.QueryRow(ctx, `
                SELECT EXISTS (
                    SELECT FROM information_schema.tables
                    WHERE table_schema = 'public' AND table_name = $1
                )
            `, table).Scan(&exists)
			require.NoError(t, err)
			assert.False(t, exists, "%s must be dropped by 000004", table)
		}
	})

	t.Run("streams.organization_id is now NOT NULL", func(t *testing.T) {
		var nullable string
		err := pool.QueryRow(ctx, `
            SELECT is_nullable FROM information_schema.columns
            WHERE table_schema = 'public'
              AND table_name = 'streams'
              AND column_name = 'organization_id'
        `).Scan(&nullable)
		require.NoError(t, err)
		assert.Equal(t, "NO", nullable, "streams.organization_id must be NOT NULL after 000005")
	})

	t.Run("organization_memberships member-requires-team CHECK is in force", func(t *testing.T) {
		// Create a team and a user, then try inserting a 'member' membership without a team_id.
		teamUserID := uuid.New()
		_, err := pool.Exec(ctx, `INSERT INTO users (id, email) VALUES ($1, $2)`, teamUserID, "teamtest@example.org")
		require.NoError(t, err)

		_, err = pool.Exec(ctx, `
            INSERT INTO organization_memberships (id, user_id, organization_id, role, team_id)
            VALUES ($1, $2, '00000000-0000-4000-8000-000000000002', 'member', NULL)
        `, uuid.New(), teamUserID)
		require.Error(t, err, "expected CHECK violation for role=member without team_id")
		assert.Contains(t, err.Error(), "organization_memberships_member_requires_team")
	})

	t.Run("room_acl_rules polymorphic trigger rejects dangling target", func(t *testing.T) {
		// First create a real room to attach rules to.
		creatorID := uuid.New()
		_, err := pool.Exec(ctx, `INSERT INTO users (id, email) VALUES ($1, $2)`, creatorID, "creator@example.org")
		require.NoError(t, err)

		roomID := uuid.New()
		_, err = pool.Exec(ctx, `
            INSERT INTO rooms (id, organization_id, name, scope, created_by_user_id)
            VALUES ($1, '00000000-0000-4000-8000-000000000002', 'Test Room', 'org', $2)
        `, roomID, creatorID)
		require.NoError(t, err)

		// Insert an ACL rule pointing at a user UUID that does not exist.
		_, err = pool.Exec(ctx, `
            INSERT INTO room_acl_rules (id, room_id, type, target_id)
            VALUES ($1, $2, 'user', $3)
        `, uuid.New(), roomID, uuid.New())
		require.Error(t, err, "polymorphic trigger must reject dangling user target")
		assert.Contains(t, err.Error(), "not found in user table")
	})

	t.Run("rooms scope/team_id consistency CHECK is in force", func(t *testing.T) {
		creatorID := uuid.New()
		_, err := pool.Exec(ctx, `INSERT INTO users (id, email) VALUES ($1, $2)`, creatorID, "scopecheck@example.org")
		require.NoError(t, err)

		// scope='org' with a non-NULL team_id must fail.
		_, err = pool.Exec(ctx, `
            INSERT INTO rooms (id, organization_id, name, scope, team_id, created_by_user_id)
            VALUES ($1, '00000000-0000-4000-8000-000000000002', 'Bad Org Room', 'org', $2, $3)
        `, uuid.New(), uuid.New(), creatorID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rooms_scope_team_consistency")
	})
}
