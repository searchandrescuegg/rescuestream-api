package database

import (
	"errors"
	"testing"
)

func TestCheckOptimisticUpdate_success(t *testing.T) {
	called := false
	err := CheckOptimisticUpdate(1, func() (int64, error) {
		called = true
		return 0, nil
	})
	if err != nil {
		t.Fatalf("expected nil error on success, got: %v", err)
	}
	if called {
		t.Fatal("fetchCurrent must not be invoked on the happy path")
	}
}

func TestCheckOptimisticUpdate_staleVersion(t *testing.T) {
	err := CheckOptimisticUpdate(0, func() (int64, error) {
		return 7, nil
	})
	var stale *ErrStaleVersion
	if !errors.As(err, &stale) {
		t.Fatalf("expected *ErrStaleVersion, got: %T %v", err, err)
	}
	if stale.CurrentVersion != 7 {
		t.Fatalf("expected CurrentVersion=7, got %d", stale.CurrentVersion)
	}
}

func TestCheckOptimisticUpdate_fetchError(t *testing.T) {
	sentinel := errors.New("db down")
	err := CheckOptimisticUpdate(0, func() (int64, error) {
		return 0, sentinel
	})
	if err == nil {
		t.Fatal("expected an error when fetchCurrent fails")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel error, got: %v", err)
	}
	if IsStaleVersion(err) {
		t.Fatal("fetch error must NOT classify as stale-version (caller cannot infer the current version)")
	}
}

func TestIsStaleVersion(t *testing.T) {
	if !IsStaleVersion(&ErrStaleVersion{CurrentVersion: 1}) {
		t.Fatal("IsStaleVersion must recognize a direct *ErrStaleVersion")
	}
	if IsStaleVersion(errors.New("other")) {
		t.Fatal("IsStaleVersion must reject unrelated errors")
	}
	if IsStaleVersion(nil) {
		t.Fatal("IsStaleVersion must reject nil")
	}
}
