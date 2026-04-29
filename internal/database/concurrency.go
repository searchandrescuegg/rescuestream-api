package database

import (
	"errors"
	"fmt"
)

// ErrStaleVersion is returned when an optimistic-concurrency UPDATE fails
// because the caller's expected version no longer matches the row.
// CurrentVersion is the row's current version, fetched after the failed
// update so the caller can include it in the RFC 9457 problem instance
// metadata (see FR-024a, tasks.md T021 `stale-room-version`).
type ErrStaleVersion struct {
	CurrentVersion int64
}

func (e *ErrStaleVersion) Error() string {
	return fmt.Sprintf("stale version (current=%d)", e.CurrentVersion)
}

// IsStaleVersion reports whether err wraps an *ErrStaleVersion.
func IsStaleVersion(err error) bool {
	var target *ErrStaleVersion
	return errors.As(err, &target)
}

// CheckOptimisticUpdate inspects the rowsAffected result of a version-guarded
// UPDATE statement (e.g. `UPDATE rooms SET …, version = version + 1
// WHERE id = $1 AND version = $2`). If the update affected at least one row,
// it returns nil. Otherwise it invokes fetchCurrent to retrieve the row's
// current version and returns an *ErrStaleVersion carrying that version.
//
// fetchCurrent is called ONLY on the stale-version path, so callers may
// pass a closure that executes an extra query without paying for it on the
// happy path.
func CheckOptimisticUpdate(rowsAffected int64, fetchCurrent func() (int64, error)) error {
	if rowsAffected >= 1 {
		return nil
	}
	cur, err := fetchCurrent()
	if err != nil {
		return fmt.Errorf("fetching current version on stale-version conflict: %w", err)
	}
	return &ErrStaleVersion{CurrentVersion: cur}
}
