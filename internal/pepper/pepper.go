// Package pepper provides deterministic, peppered HMAC-SHA256 hashing for
// high-entropy credentials (device keys, session HMAC secrets).
//
// The pepper is a server-side secret loaded from environment (Fly secrets in
// production, .env locally). It never appears in the database, so a database
// dump alone is insufficient to recover plaintexts: an attacker would need
// both the database and the current pepper.
//
// Rotation: construct a Hasher with WithPrevious(previousPepper) during a
// rotation window and use VerifyWithPrev on the verification path. A
// background rekey pass can re-hash matching rows under the new pepper.
package pepper

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// minPepperLen is the minimum length in characters for any configured pepper.
// 32 characters of a base64-encoded random source gives ~24 bytes of entropy,
// which exceeds the practical threshold for the HMAC threat model.
const minPepperLen = 32

// Hasher computes and verifies peppered HMAC-SHA256 hashes. Safe for
// concurrent use.
type Hasher struct {
	current []byte
	prev    []byte // nil outside of a rotation window
}

// Option configures a Hasher at construction time.
type Option func(*Hasher) error

// WithPrevious enables verification against a previous pepper during a
// rotation window. Pass an empty string as a no-op.
func WithPrevious(prev string) Option {
	return func(h *Hasher) error {
		if prev == "" {
			return nil
		}
		if len(prev) < minPepperLen {
			return fmt.Errorf("pepper: previous pepper must be at least %d characters", minPepperLen)
		}
		h.prev = []byte(prev)
		return nil
	}
}

// New constructs a Hasher with the given current pepper. The pepper's raw
// bytes are used as the HMAC key, so any consistent encoding (base64, hex,
// raw) works as long as the same value is reused across restarts.
func New(current string, opts ...Option) (*Hasher, error) {
	if current == "" {
		return nil, errors.New("pepper: current pepper must not be empty")
	}
	if len(current) < minPepperLen {
		return nil, fmt.Errorf("pepper: current pepper must be at least %d characters", minPepperLen)
	}
	h := &Hasher{current: []byte(current)}
	for _, o := range opts {
		if err := o(h); err != nil {
			return nil, err
		}
	}
	return h, nil
}

// Hash returns the hex-encoded HMAC-SHA256 of plain under the current pepper.
func (h *Hasher) Hash(plain string) string {
	return hex.EncodeToString(computeMAC(h.current, plain))
}

// Verify reports whether hashHex matches plain under the current pepper,
// using constant-time comparison.
func (h *Hasher) Verify(plain, hashHex string) bool {
	want, err := hex.DecodeString(hashHex)
	if err != nil {
		return false
	}
	return hmac.Equal(computeMAC(h.current, plain), want)
}

// VerifyWithPrev reports whether hashHex matches plain under either the
// current or previous pepper. matchedPrev is true iff the match came from
// the previous pepper — callers use this signal to schedule a rekey.
func (h *Hasher) VerifyWithPrev(plain, hashHex string) (ok, matchedPrev bool) {
	want, err := hex.DecodeString(hashHex)
	if err != nil {
		return false, false
	}
	if hmac.Equal(computeMAC(h.current, plain), want) {
		return true, false
	}
	if h.prev == nil {
		return false, false
	}
	if hmac.Equal(computeMAC(h.prev, plain), want) {
		return true, true
	}
	return false, false
}

func computeMAC(key []byte, plain string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(plain))
	return mac.Sum(nil)
}
