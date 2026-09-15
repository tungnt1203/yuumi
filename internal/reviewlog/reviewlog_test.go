package reviewlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileLogger_WritesOneJSONFilePerCall(t *testing.T) {
	dir := t.TempDir()
	logger := NewFileLogger(dir)

	logger.LogReview("owner/repo", 42, "abc123", 1, 2, "prompt gửi đi", "response nhận được", "", 1500*time.Millisecond)

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(files))
	}

	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("cannot unmarshal log file: %v", err)
	}

	if e.RepoFullName != "owner/repo" || e.IssueNumber != 42 || e.SHA != "abc123" {
		t.Errorf("unexpected identity fields, got: %+v", e)
	}
	if e.BundleIndex != 1 || e.BundleTotal != 2 {
		t.Errorf("expected bundle 1/2, got %d/%d", e.BundleIndex, e.BundleTotal)
	}
	if e.Prompt != "prompt gửi đi" || e.Response != "response nhận được" {
		t.Errorf("unexpected prompt/response, got: %+v", e)
	}
	if e.Error != "" {
		t.Errorf("expected no error, got: %q", e.Error)
	}
	if e.DurationMs != 1500 {
		t.Errorf("expected duration_ms=1500, got %d", e.DurationMs)
	}
}

func TestFileLogger_CreatesDirIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs")
	logger := NewFileLogger(dir)

	logger.LogReview("owner/repo", 1, "sha", 1, 1, "p", "r", "", time.Second)

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("expected log dir to be created, got error: %v", err)
	}
}

func TestFileLogger_MultipleCalls_WriteSeparateFiles(t *testing.T) {
	dir := t.TempDir()
	logger := NewFileLogger(dir)

	logger.LogReview("owner/repo", 1, "sha", 1, 2, "p1", "r1", "", time.Second)
	logger.LogReview("owner/repo", 1, "sha", 2, 2, "p2", "r2", "", time.Second)

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 separate log files, got %d", len(files))
	}
}
