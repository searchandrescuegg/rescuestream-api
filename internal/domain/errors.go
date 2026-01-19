package domain

import "errors"

// Domain errors
var (
	// ErrNotFound indicates the requested entity was not found.
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists indicates an entity with the same unique key already exists.
	ErrAlreadyExists = errors.New("already exists")

	// ErrInvalidStatus indicates an invalid status transition was attempted.
	ErrInvalidStatus = errors.New("invalid status")

	// ErrStreamKeyInUse indicates the stream key is already in use by an active stream.
	ErrStreamKeyInUse = errors.New("stream key in use")

	// ErrStreamKeyRevoked indicates the stream key has been revoked.
	ErrStreamKeyRevoked = errors.New("stream key revoked")

	// ErrStreamKeyExpired indicates the stream key has expired.
	ErrStreamKeyExpired = errors.New("stream key expired")

	// ErrInvalidStreamKey indicates the stream key is invalid.
	ErrInvalidStreamKey = errors.New("invalid stream key")

	// ErrUnauthorized indicates the request is not authorized.
	ErrUnauthorized = errors.New("unauthorized")
)
