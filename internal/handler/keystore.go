package handler

import (
	"errors"

	"github.com/searchandrescuegg/rescuestream-api/internal/domain"
)

// StaticKeyStore is a simple key store that uses a single API key/secret pair.
type StaticKeyStore struct {
	apiKey string
	secret string
}

// NewStaticKeyStore creates a new static key store with a single API key/secret pair.
func NewStaticKeyStore(apiKey, secret string) *StaticKeyStore {
	return &StaticKeyStore{
		apiKey: apiKey,
		secret: secret,
	}
}

// GetSecret returns the secret for the given API key.
func (s *StaticKeyStore) GetSecret(apiKey string) (string, error) {
	if apiKey == s.apiKey {
		return s.secret, nil
	}
	return "", domain.ErrUnauthorized
}

// EnvKeyStore uses environment-based configuration for keys.
// It maps the API_SECRET env var to a single "admin" key.
type EnvKeyStore struct {
	secret string
}

// NewEnvKeyStore creates a key store using the API_SECRET environment variable.
func NewEnvKeyStore(secret string) *EnvKeyStore {
	return &EnvKeyStore{
		secret: secret,
	}
}

// GetSecret returns the secret for the given API key.
// For EnvKeyStore, any non-empty API key will use the same secret.
func (s *EnvKeyStore) GetSecret(apiKey string) (string, error) {
	if apiKey == "" {
		return "", errors.New("empty API key")
	}
	return s.secret, nil
}
