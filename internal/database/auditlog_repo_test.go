package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/searchandrescuegg/rescuestream-api/internal/database"
	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
	"github.com/searchandrescuegg/rescuestream-api/internal/testutil"
)

// ============================================================
// Tests for AuditLogRepo.Create (T013)
// ============================================================

func TestAuditLogRepo_Create(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	entry := &domain.AuditLogEntry{
		ID:            uuid.New(),
		Timestamp:     time.Now(),
		Actor:         "test-api-key",
		Action:        "create",
		ResourceType:  stringPtr("broadcaster"),
		ResourceID:    uuidPtr(uuid.New()),
		RequestMethod: "POST",
		RequestPath:   "/broadcasters",
		IPAddress:     "192.168.1.1",
		Outcome:       "success",
		Metadata:      map[string]interface{}{"key": "value"},
		RequestID:     stringPtr("req-123"),
	}

	err := repo.Create(context.Background(), entry)
	require.NoError(t, err)

	// Verify entry was created
	entries, total, err := repo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)

	created := entries[0]
	assert.Equal(t, entry.ID, created.ID)
	assert.Equal(t, entry.Actor, created.Actor)
	assert.Equal(t, entry.Action, created.Action)
	assert.Equal(t, *entry.ResourceType, *created.ResourceType)
	assert.Equal(t, *entry.ResourceID, *created.ResourceID)
	assert.Equal(t, entry.RequestMethod, created.RequestMethod)
	assert.Equal(t, entry.RequestPath, created.RequestPath)
	assert.Equal(t, entry.IPAddress, created.IPAddress)
	assert.Equal(t, entry.Outcome, created.Outcome)
	assert.Equal(t, entry.Metadata["key"], created.Metadata["key"])
	assert.Equal(t, *entry.RequestID, *created.RequestID)
}

func TestAuditLogRepo_Create_WithNilOptionalFields(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	entry := &domain.AuditLogEntry{
		ID:            uuid.New(),
		Timestamp:     time.Now(),
		Actor:         "test-api-key",
		Action:        "login",
		ResourceType:  nil, // Optional
		ResourceID:    nil, // Optional
		RequestMethod: "POST",
		RequestPath:   "/audit-events",
		IPAddress:     "127.0.0.1",
		Outcome:       "success",
		Metadata:      map[string]interface{}{},
		RequestID:     nil, // Optional
		FailureReason: nil, // Optional
	}

	err := repo.Create(context.Background(), entry)
	require.NoError(t, err)

	entries, _, err := repo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Nil(t, entries[0].ResourceType)
	assert.Nil(t, entries[0].ResourceID)
	assert.Nil(t, entries[0].RequestID)
	assert.Nil(t, entries[0].FailureReason)
}

func TestAuditLogRepo_Create_WithFailureReason(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	entry := &domain.AuditLogEntry{
		ID:            uuid.New(),
		Timestamp:     time.Now(),
		Actor:         "test-api-key",
		Action:        "create",
		RequestMethod: "POST",
		RequestPath:   "/broadcasters",
		IPAddress:     "192.168.1.1",
		Outcome:       "failure",
		FailureReason: stringPtr("HTTP 400"),
		Metadata:      map[string]interface{}{},
	}

	err := repo.Create(context.Background(), entry)
	require.NoError(t, err)

	entries, _, err := repo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, "failure", entries[0].Outcome)
	assert.NotNil(t, entries[0].FailureReason)
	assert.Equal(t, "HTTP 400", *entries[0].FailureReason)
}

func TestAuditLogRepo_Create_GeneratesID(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	entry := &domain.AuditLogEntry{
		// ID not set - should be generated
		Timestamp:     time.Now(),
		Actor:         "test-api-key",
		Action:        "create",
		RequestMethod: "POST",
		RequestPath:   "/broadcasters",
		IPAddress:     "192.168.1.1",
		Outcome:       "success",
		Metadata:      map[string]interface{}{},
	}

	err := repo.Create(context.Background(), entry)
	require.NoError(t, err)

	// ID should have been assigned
	assert.NotEqual(t, uuid.Nil, entry.ID)
}

func TestAuditLogRepo_Create_WithComplexMetadata(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	entry := &domain.AuditLogEntry{
		ID:            uuid.New(),
		Timestamp:     time.Now(),
		Actor:         "test-api-key",
		Action:        "create",
		RequestMethod: "POST",
		RequestPath:   "/broadcasters",
		IPAddress:     "192.168.1.1",
		Outcome:       "success",
		Metadata: map[string]interface{}{
			"string_field": "value",
			"int_field":    42,
			"bool_field":   true,
			"nested": map[string]interface{}{
				"inner": "data",
			},
		},
	}

	err := repo.Create(context.Background(), entry)
	require.NoError(t, err)

	entries, _, err := repo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, "value", entries[0].Metadata["string_field"])
	// JSON unmarshals numbers as float64
	assert.Equal(t, float64(42), entries[0].Metadata["int_field"])
	assert.Equal(t, true, entries[0].Metadata["bool_field"])
}

// ============================================================
// Tests for AuditLogRepo.List (T023)
// ============================================================

func TestAuditLogRepo_List_ReturnsEmptyForNoEntries(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	entries, total, err := repo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)

	assert.Empty(t, entries)
	assert.Equal(t, int64(0), total)
}

func TestAuditLogRepo_List_ReturnsEntriesOrderedByTimestampDesc(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	// Create entries with different timestamps
	now := time.Now()
	entries := []*domain.AuditLogEntry{
		createEntry("actor", "action1", now.Add(-2*time.Hour)),
		createEntry("actor", "action2", now.Add(-1*time.Hour)),
		createEntry("actor", "action3", now),
	}

	for _, entry := range entries {
		require.NoError(t, repo.Create(context.Background(), entry))
	}

	result, total, err := repo.List(context.Background(), domain.AuditLogFilter{Limit: 10})
	require.NoError(t, err)

	assert.Equal(t, int64(3), total)
	assert.Len(t, result, 3)

	// Should be ordered by timestamp desc (newest first)
	assert.Equal(t, "action3", result[0].Action)
	assert.Equal(t, "action2", result[1].Action)
	assert.Equal(t, "action1", result[2].Action)
}

func TestAuditLogRepo_List_ReturnsCorrectTotal(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	// Create 15 entries
	for i := 0; i < 15; i++ {
		entry := createEntry("actor", "action", time.Now())
		require.NoError(t, repo.Create(context.Background(), entry))
	}

	// Request only 5 with limit
	result, total, err := repo.List(context.Background(), domain.AuditLogFilter{Limit: 5})
	require.NoError(t, err)

	assert.Len(t, result, 5)
	assert.Equal(t, int64(15), total) // Total should reflect all matching records
}

// ============================================================
// Tests for Filtered Queries (T040)
// ============================================================

func TestAuditLogRepo_List_FilterByActor(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	// Create entries with different actors
	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(context.Background(), createEntry("actor-1", "action", time.Now())))
	}
	for i := 0; i < 2; i++ {
		require.NoError(t, repo.Create(context.Background(), createEntry("actor-2", "action", time.Now())))
	}

	actor := "actor-1"
	result, total, err := repo.List(context.Background(), domain.AuditLogFilter{
		Actor: &actor,
		Limit: 10,
	})
	require.NoError(t, err)

	assert.Len(t, result, 3)
	assert.Equal(t, int64(3), total)
	for _, entry := range result {
		assert.Equal(t, "actor-1", entry.Actor)
	}
}

func TestAuditLogRepo_List_FilterByAction(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	require.NoError(t, repo.Create(context.Background(), createEntry("actor", "create", time.Now())))
	require.NoError(t, repo.Create(context.Background(), createEntry("actor", "update", time.Now())))
	require.NoError(t, repo.Create(context.Background(), createEntry("actor", "create", time.Now())))

	action := "create"
	result, total, err := repo.List(context.Background(), domain.AuditLogFilter{
		Action: &action,
		Limit:  10,
	})
	require.NoError(t, err)

	assert.Len(t, result, 2)
	assert.Equal(t, int64(2), total)
	for _, entry := range result {
		assert.Equal(t, "create", entry.Action)
	}
}

func TestAuditLogRepo_List_FilterByResourceType(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	entry1 := createEntry("actor", "create", time.Now())
	entry1.ResourceType = stringPtr("broadcaster")
	require.NoError(t, repo.Create(context.Background(), entry1))

	entry2 := createEntry("actor", "create", time.Now())
	entry2.ResourceType = stringPtr("stream")
	require.NoError(t, repo.Create(context.Background(), entry2))

	entry3 := createEntry("actor", "create", time.Now())
	entry3.ResourceType = stringPtr("broadcaster")
	require.NoError(t, repo.Create(context.Background(), entry3))

	resourceType := "broadcaster"
	result, total, err := repo.List(context.Background(), domain.AuditLogFilter{
		ResourceType: &resourceType,
		Limit:        10,
	})
	require.NoError(t, err)

	assert.Len(t, result, 2)
	assert.Equal(t, int64(2), total)
	for _, entry := range result {
		assert.Equal(t, "broadcaster", *entry.ResourceType)
	}
}

func TestAuditLogRepo_List_FilterByResourceID(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	targetID := uuid.New()
	otherID := uuid.New()

	entry1 := createEntry("actor", "create", time.Now())
	entry1.ResourceID = &targetID
	require.NoError(t, repo.Create(context.Background(), entry1))

	entry2 := createEntry("actor", "update", time.Now())
	entry2.ResourceID = &otherID
	require.NoError(t, repo.Create(context.Background(), entry2))

	entry3 := createEntry("actor", "delete", time.Now())
	entry3.ResourceID = &targetID
	require.NoError(t, repo.Create(context.Background(), entry3))

	result, total, err := repo.List(context.Background(), domain.AuditLogFilter{
		ResourceID: &targetID,
		Limit:      10,
	})
	require.NoError(t, err)

	assert.Len(t, result, 2)
	assert.Equal(t, int64(2), total)
	for _, entry := range result {
		assert.Equal(t, targetID, *entry.ResourceID)
	}
}

func TestAuditLogRepo_List_FilterByDateRange(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	twoDaysAgo := now.Add(-48 * time.Hour)

	// Create entries at different times
	require.NoError(t, repo.Create(context.Background(), createEntry("actor", "old", twoDaysAgo)))
	require.NoError(t, repo.Create(context.Background(), createEntry("actor", "yesterday", yesterday)))
	require.NoError(t, repo.Create(context.Background(), createEntry("actor", "now", now)))

	// Filter from yesterday to now
	from := yesterday.Add(-1 * time.Minute)
	result, total, err := repo.List(context.Background(), domain.AuditLogFilter{
		From:  &from,
		Limit: 10,
	})
	require.NoError(t, err)

	assert.Len(t, result, 2)
	assert.Equal(t, int64(2), total)
}

func TestAuditLogRepo_List_FilterByDateRangeTo(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	twoDaysAgo := now.Add(-48 * time.Hour)

	require.NoError(t, repo.Create(context.Background(), createEntry("actor", "old", twoDaysAgo)))
	require.NoError(t, repo.Create(context.Background(), createEntry("actor", "yesterday", yesterday)))
	require.NoError(t, repo.Create(context.Background(), createEntry("actor", "now", now)))

	// Filter entries before yesterday
	to := yesterday.Add(-1 * time.Minute)
	result, total, err := repo.List(context.Background(), domain.AuditLogFilter{
		To:    &to,
		Limit: 10,
	})
	require.NoError(t, err)

	assert.Len(t, result, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "old", result[0].Action)
}

func TestAuditLogRepo_List_MultipleFilters(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	// Create various entries
	entry1 := createEntry("admin", "create", time.Now())
	entry1.ResourceType = stringPtr("broadcaster")
	require.NoError(t, repo.Create(context.Background(), entry1))

	entry2 := createEntry("admin", "update", time.Now())
	entry2.ResourceType = stringPtr("broadcaster")
	require.NoError(t, repo.Create(context.Background(), entry2))

	entry3 := createEntry("user", "create", time.Now())
	entry3.ResourceType = stringPtr("broadcaster")
	require.NoError(t, repo.Create(context.Background(), entry3))

	entry4 := createEntry("admin", "create", time.Now())
	entry4.ResourceType = stringPtr("stream")
	require.NoError(t, repo.Create(context.Background(), entry4))

	// Filter by actor=admin AND action=create AND resource_type=broadcaster
	actor := "admin"
	action := "create"
	resourceType := "broadcaster"
	result, total, err := repo.List(context.Background(), domain.AuditLogFilter{
		Actor:        &actor,
		Action:       &action,
		ResourceType: &resourceType,
		Limit:        10,
	})
	require.NoError(t, err)

	assert.Len(t, result, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "admin", result[0].Actor)
	assert.Equal(t, "create", result[0].Action)
	assert.Equal(t, "broadcaster", *result[0].ResourceType)
}

func TestAuditLogRepo_List_Pagination(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	// Create 10 entries
	for i := 0; i < 10; i++ {
		entry := createEntry("actor", "action", time.Now().Add(-time.Duration(i)*time.Second))
		require.NoError(t, repo.Create(context.Background(), entry))
	}

	// First page
	page1, total, err := repo.List(context.Background(), domain.AuditLogFilter{
		Limit:  3,
		Offset: 0,
	})
	require.NoError(t, err)
	assert.Len(t, page1, 3)
	assert.Equal(t, int64(10), total)

	// Second page
	page2, total, err := repo.List(context.Background(), domain.AuditLogFilter{
		Limit:  3,
		Offset: 3,
	})
	require.NoError(t, err)
	assert.Len(t, page2, 3)
	assert.Equal(t, int64(10), total)

	// Verify no overlap
	for _, p1Entry := range page1 {
		for _, p2Entry := range page2 {
			assert.NotEqual(t, p1Entry.ID, p2Entry.ID)
		}
	}
}

func TestAuditLogRepo_List_LargeOffset(t *testing.T) {
	db := testutil.SetupTestDatabase(t)
	defer db.Cleanup(t)

	repo := database.NewAuditLogRepo(db.Pool)

	// Create 5 entries
	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(context.Background(), createEntry("actor", "action", time.Now())))
	}

	// Offset beyond total
	result, total, err := repo.List(context.Background(), domain.AuditLogFilter{
		Limit:  10,
		Offset: 100,
	})
	require.NoError(t, err)

	assert.Empty(t, result)
	assert.Equal(t, int64(5), total) // Total should still reflect actual count
}

// ============================================================
// Helper Functions
// ============================================================

func createEntry(actor, action string, timestamp time.Time) *domain.AuditLogEntry {
	return &domain.AuditLogEntry{
		ID:            uuid.New(),
		Timestamp:     timestamp,
		Actor:         actor,
		Action:        action,
		RequestMethod: "POST",
		RequestPath:   "/test",
		IPAddress:     "127.0.0.1",
		Outcome:       "success",
		Metadata:      map[string]interface{}{},
	}
}

func stringPtr(s string) *string {
	return &s
}

func uuidPtr(u uuid.UUID) *uuid.UUID {
	return &u
}
