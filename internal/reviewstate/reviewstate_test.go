package reviewstate

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(filepath.Join(t.TempDir(), "state.json"))
}

func TestFileStore_LastReviewedSHA_NoFile_NotFoundNoError(t *testing.T) {
	s := newTestStore(t)

	sha, found, err := s.LastReviewedSHA("owner/repo", 1)
	if err != nil {
		t.Fatalf("LastReviewedSHA() unexpected error: %v", err)
	}
	if found {
		t.Errorf("found = true, want false (no state file yet)")
	}
	if sha != "" {
		t.Errorf("sha = %q, want empty", sha)
	}
}

func TestFileStore_SetThenGet_RoundTrips(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetLastReviewedSHA("owner/repo", 42, "abc123"); err != nil {
		t.Fatalf("SetLastReviewedSHA() unexpected error: %v", err)
	}

	sha, found, err := s.LastReviewedSHA("owner/repo", 42)
	if err != nil {
		t.Fatalf("LastReviewedSHA() unexpected error: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if sha != "abc123" {
		t.Errorf("sha = %q, want %q", sha, "abc123")
	}
}

func TestFileStore_DistinctKeysDoNotClash(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetLastReviewedSHA("owner/repo-a", 1, "sha-a"); err != nil {
		t.Fatalf("SetLastReviewedSHA() unexpected error: %v", err)
	}
	if err := s.SetLastReviewedSHA("owner/repo-a", 2, "sha-b"); err != nil {
		t.Fatalf("SetLastReviewedSHA() unexpected error: %v", err)
	}
	if err := s.SetLastReviewedSHA("owner/repo-b", 1, "sha-c"); err != nil {
		t.Fatalf("SetLastReviewedSHA() unexpected error: %v", err)
	}

	cases := []struct {
		repo      string
		issue     int
		wantSHA   string
		wantFound bool
	}{
		{"owner/repo-a", 1, "sha-a", true},
		{"owner/repo-a", 2, "sha-b", true},
		{"owner/repo-b", 1, "sha-c", true},
		{"owner/repo-b", 2, "", false},
	}
	for _, c := range cases {
		sha, found, err := s.LastReviewedSHA(c.repo, c.issue)
		if err != nil {
			t.Fatalf("LastReviewedSHA(%q, %d) unexpected error: %v", c.repo, c.issue, err)
		}
		if found != c.wantFound || sha != c.wantSHA {
			t.Errorf("LastReviewedSHA(%q, %d) = (%q, %v), want (%q, %v)", c.repo, c.issue, sha, found, c.wantSHA, c.wantFound)
		}
	}
}

func TestFileStore_SetOverwritesPreviousSHA(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetLastReviewedSHA("owner/repo", 1, "sha-1"); err != nil {
		t.Fatalf("SetLastReviewedSHA() unexpected error: %v", err)
	}
	if err := s.SetLastReviewedSHA("owner/repo", 1, "sha-2"); err != nil {
		t.Fatalf("SetLastReviewedSHA() unexpected error: %v", err)
	}

	sha, found, err := s.LastReviewedSHA("owner/repo", 1)
	if err != nil {
		t.Fatalf("LastReviewedSHA() unexpected error: %v", err)
	}
	if !found || sha != "sha-2" {
		t.Errorf("LastReviewedSHA() = (%q, %v), want (%q, true)", sha, found, "sha-2")
	}
}

func TestFileStore_CreatesMissingDirectory(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "nested", "dir", "state.json"))

	if err := s.SetLastReviewedSHA("owner/repo", 1, "sha-1"); err != nil {
		t.Fatalf("SetLastReviewedSHA() unexpected error: %v", err)
	}
	if _, err := os.Stat(s.Path); err != nil {
		t.Errorf("state file not created: %v", err)
	}
}

func TestFileStore_LastReviewedSHA_InvalidJSON_ReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("cannot write test file: %v", err)
	}
	s := NewFileStore(path)

	_, _, err := s.LastReviewedSHA("owner/repo", 1)
	if err == nil {
		t.Fatal("LastReviewedSHA() expected error on invalid JSON, got nil")
	}
}

func TestFileStore_SetLastReviewedSHA_InvalidExistingJSON_StillWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("cannot write test file: %v", err)
	}
	s := NewFileStore(path)

	// File cũ hỏng không được chặn ghi state mới (xem doc comment SetLastReviewedSHA).
	if err := s.SetLastReviewedSHA("owner/repo", 1, "sha-1"); err != nil {
		t.Fatalf("SetLastReviewedSHA() unexpected error: %v", err)
	}

	sha, found, err := s.LastReviewedSHA("owner/repo", 1)
	if err != nil {
		t.Fatalf("LastReviewedSHA() unexpected error: %v", err)
	}
	if !found || sha != "sha-1" {
		t.Errorf("LastReviewedSHA() = (%q, %v), want (%q, true)", sha, found, "sha-1")
	}
}

func TestFileStore_ConcurrentSets_NoDataRace(t *testing.T) {
	s := newTestStore(t)

	var wg sync.WaitGroup
	for n := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = s.SetLastReviewedSHA("owner/repo", n, "sha")
		}(n)
	}
	wg.Wait()

	for i := range 20 {
		if _, found, err := s.LastReviewedSHA("owner/repo", i); err != nil || !found {
			t.Errorf("issue %d: found=%v err=%v, want found=true no error", i, found, err)
		}
	}
}
