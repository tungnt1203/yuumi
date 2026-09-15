package review

import (
	"errors"
	"testing"
)

func TestAlreadyReviewedSHA_MatchingSHA_ReturnsTrue(t *testing.T) {
	store := newFakeStateStore(map[string]string{stateKey("octo/repo", 1): "abc123"})

	if !AlreadyReviewedSHA(store, "octo/repo", 1, "abc123") {
		t.Error("expected true when last reviewed SHA matches")
	}
}

func TestAlreadyReviewedSHA_DifferentSHA_ReturnsFalse(t *testing.T) {
	store := newFakeStateStore(map[string]string{stateKey("octo/repo", 1): "abc123"})

	if AlreadyReviewedSHA(store, "octo/repo", 1, "def456") {
		t.Error("expected false when SHA differs (new commits pushed)")
	}
}

func TestAlreadyReviewedSHA_NotFound_ReturnsFalse(t *testing.T) {
	store := newFakeStateStore(nil)

	if AlreadyReviewedSHA(store, "octo/repo", 1, "abc123") {
		t.Error("expected false when PR was never reviewed before")
	}
}

func TestAlreadyReviewedSHA_NilStore_ReturnsFalse(t *testing.T) {
	if AlreadyReviewedSHA(nil, "octo/repo", 1, "abc123") {
		t.Error("expected false when StateStore is not configured")
	}
}

func TestAlreadyReviewedSHA_EmptySHA_ReturnsFalse(t *testing.T) {
	store := newFakeStateStore(map[string]string{stateKey("octo/repo", 1): ""})

	if AlreadyReviewedSHA(store, "octo/repo", 1, "") {
		t.Error("expected false for empty sha — never safe to skip review with no SHA to compare")
	}
}

func TestAlreadyReviewedSHA_LookupError_ReturnsFalse(t *testing.T) {
	store := newFakeStateStore(nil)
	store.lastSHAErr = errors.New("boom")

	if AlreadyReviewedSHA(store, "octo/repo", 1, "abc123") {
		t.Error("expected false when the state lookup errors — safer to review than to silently skip")
	}
}
