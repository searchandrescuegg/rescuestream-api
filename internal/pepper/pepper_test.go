package pepper

import (
	"strings"
	"testing"
)

const (
	pepperA = "test-pepper-aaaaaaaaaaaaaaaaaaaaaaaa"
	pepperB = "test-pepper-bbbbbbbbbbbbbbbbbbbbbbbb"
)

func mustHasher(t *testing.T, current string, opts ...Option) *Hasher {
	t.Helper()
	h, err := New(current, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func TestNew_rejectsEmpty(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("expected error for empty pepper")
	}
}

func TestNew_rejectsShort(t *testing.T) {
	if _, err := New("short"); err == nil {
		t.Fatal("expected error for short pepper")
	}
	if _, err := New(strings.Repeat("x", minPepperLen-1)); err == nil {
		t.Fatal("expected error for pepper below minimum length")
	}
}

func TestNew_acceptsMinLength(t *testing.T) {
	if _, err := New(strings.Repeat("x", minPepperLen)); err != nil {
		t.Fatalf("expected minimum-length pepper to be accepted: %v", err)
	}
}

func TestWithPrevious_rejectsShort(t *testing.T) {
	if _, err := New(pepperA, WithPrevious("short")); err == nil {
		t.Fatal("expected error for short previous pepper")
	}
}

func TestWithPrevious_emptyIsNoOp(t *testing.T) {
	h := mustHasher(t, pepperA, WithPrevious(""))
	if h.prev != nil {
		t.Fatal("empty previous pepper should leave prev nil")
	}
}

func TestHash_deterministic(t *testing.T) {
	h := mustHasher(t, pepperA)
	got1 := h.Hash("device-key-plaintext")
	got2 := h.Hash("device-key-plaintext")
	if got1 != got2 {
		t.Fatalf("Hash not deterministic: %q vs %q", got1, got2)
	}
}

func TestHash_differentPeppersDifferentOutputs(t *testing.T) {
	hA := mustHasher(t, pepperA)
	hB := mustHasher(t, pepperB)
	if hA.Hash("same-input") == hB.Hash("same-input") {
		t.Fatal("different peppers must produce different hashes for the same input")
	}
}

func TestVerify_matchAndMismatch(t *testing.T) {
	h := mustHasher(t, pepperA)
	hashed := h.Hash("device-key-plaintext")

	if !h.Verify("device-key-plaintext", hashed) {
		t.Fatal("expected match")
	}
	if h.Verify("other-plaintext", hashed) {
		t.Fatal("expected mismatch on different plaintext")
	}
}

func TestVerify_rejectsMalformedHash(t *testing.T) {
	h := mustHasher(t, pepperA)
	if h.Verify("anything", "not-hex") {
		t.Fatal("non-hex hash must not verify")
	}
	if h.Verify("anything", "") {
		t.Fatal("empty hash must not verify")
	}
}

func TestVerifyWithPrev_currentStillMatches(t *testing.T) {
	h := mustHasher(t, pepperA, WithPrevious(pepperB))
	hashed := h.Hash("plain")

	ok, matchedPrev := h.VerifyWithPrev("plain", hashed)
	if !ok {
		t.Fatal("expected match via current")
	}
	if matchedPrev {
		t.Fatal("expected matchedPrev=false when current is the one that matched")
	}
}

func TestVerifyWithPrev_fallsBackToPrev(t *testing.T) {
	// Hash was computed under pepperB (the previous pepper), now we've rotated to pepperA.
	hWhenB := mustHasher(t, pepperB)
	hashed := hWhenB.Hash("plain")

	h := mustHasher(t, pepperA, WithPrevious(pepperB))
	ok, matchedPrev := h.VerifyWithPrev("plain", hashed)
	if !ok {
		t.Fatal("expected match via previous pepper")
	}
	if !matchedPrev {
		t.Fatal("expected matchedPrev=true")
	}
}

func TestVerifyWithPrev_noPrev(t *testing.T) {
	h := mustHasher(t, pepperA)
	ok, matchedPrev := h.VerifyWithPrev("plain", "00")
	if ok || matchedPrev {
		t.Fatal("expected no match when no previous pepper is configured")
	}
}

func TestVerifyWithPrev_neitherMatches(t *testing.T) {
	h := mustHasher(t, pepperA, WithPrevious(pepperB))
	unrelatedHash := mustHasher(t, "unrelated-pepper-zzzzzzzzzzzzzzzzzzz").Hash("plain")

	ok, _ := h.VerifyWithPrev("plain", unrelatedHash)
	if ok {
		t.Fatal("expected no match when neither current nor previous pepper matches")
	}
}
