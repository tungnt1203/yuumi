package reviewlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tungnt1203/yuumi/internal/review"
)

// writeEntry ghi 1 file log thô, để test Summarize độc lập với FileLogger
// (FileLogger luôn dùng time.Now(), không cố định được ngày).
func writeEntry(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSummarize_GroupsByRepoAndDay(t *testing.T) {
	dir := t.TempDir()
	writeEntry(t, dir, "1.json", `{"time":"2026-09-20T10:00:00Z","repo_full_name":"a/x","usage":{"input_tokens":1,"cache_creation_input_tokens":10,"cache_read_input_tokens":100,"output_tokens":5,"cost_usd":0.5}}`)
	writeEntry(t, dir, "2.json", `{"time":"2026-09-20T11:00:00Z","repo_full_name":"b/y","usage":{"input_tokens":2,"output_tokens":6,"cost_usd":0.25}}`)
	writeEntry(t, dir, "3.json", `{"time":"2026-09-21T09:00:00Z","repo_full_name":"a/x","usage":{"input_tokens":3,"output_tokens":7,"cost_usd":1}}`)

	s, err := Summarize(dir, time.UTC)
	if err != nil {
		t.Fatalf("Summarize() error: %v", err)
	}

	wantTotal := Totals{Calls: 3, InputTokens: 6, CacheCreationInputTokens: 10, CacheReadInputTokens: 100, OutputTokens: 18, CostUSD: 1.75}
	if s.Total != wantTotal {
		t.Errorf("Total = %+v, want %+v", s.Total, wantTotal)
	}
	if got := s.ByRepo["a/x"]; got.Calls != 2 || got.CostUSD != 1.5 {
		t.Errorf("ByRepo[a/x] = %+v, want 2 calls, cost 1.5", got)
	}
	if got := s.ByRepo["b/y"]; got.Calls != 1 || got.CostUSD != 0.25 {
		t.Errorf("ByRepo[b/y] = %+v, want 1 call, cost 0.25", got)
	}
	if got := s.ByDay["2026-09-20"]; got.Calls != 2 || got.CostUSD != 0.75 {
		t.Errorf("ByDay[2026-09-20] = %+v, want 2 calls, cost 0.75", got)
	}
	if got := s.ByDay["2026-09-21"]; got.Calls != 1 || got.CostUSD != 1 {
		t.Errorf("ByDay[2026-09-21] = %+v, want 1 call, cost 1", got)
	}
}

// Ngày được tính theo múi giờ truyền vào: 20:00 UTC đã là ngày hôm sau ở
// UTC+7.
func TestSummarize_DayUsesLocation(t *testing.T) {
	dir := t.TempDir()
	writeEntry(t, dir, "1.json", `{"time":"2026-09-20T20:00:00Z","repo_full_name":"a/x"}`)

	s, err := Summarize(dir, time.FixedZone("ICT", 7*3600))
	if err != nil {
		t.Fatalf("Summarize() error: %v", err)
	}
	if _, ok := s.ByDay["2026-09-21"]; !ok {
		t.Errorf("ByDay = %v, want key 2026-09-21", s.ByDay)
	}
}

// Log ghi trước khi có usage vẫn được đếm số lần gọi; file hỏng và file
// không phải .json không làm Summarize lỗi.
func TestSummarize_OldAndBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	writeEntry(t, dir, "old.json", `{"time":"2026-09-20T10:00:00Z","repo_full_name":"a/x","attempts":1}`)
	writeEntry(t, dir, "broken.json", `{not json`)
	writeEntry(t, dir, "notes.txt", `ignore me`)

	s, err := Summarize(dir, time.UTC)
	if err != nil {
		t.Fatalf("Summarize() error: %v", err)
	}
	if s.Total != (Totals{Calls: 1}) {
		t.Errorf("Total = %+v, want 1 call with zero usage", s.Total)
	}
	if s.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", s.Skipped)
	}
}

func TestSummarize_MissingDir_ReturnsError(t *testing.T) {
	if _, err := Summarize(filepath.Join(t.TempDir(), "nope"), time.UTC); err == nil {
		t.Fatal("Summarize() on missing dir: expected error, got nil")
	}
}

// Summarize đọc được đúng file mà FileLogger ghi ra.
func TestSummarize_ReadsFileLoggerOutput(t *testing.T) {
	dir := t.TempDir()
	logger := NewFileLogger(dir)
	logger.LogReview("a/x", 1, "sha", 1, 1, "p", "r", "", time.Second, review.CallStats{Attempts: 1, Usage: review.Usage{CostUSD: 0.3}})

	s, err := Summarize(dir, time.UTC)
	if err != nil {
		t.Fatalf("Summarize() error: %v", err)
	}
	if got := s.ByRepo["a/x"]; got.Calls != 1 || got.CostUSD != 0.3 {
		t.Errorf("ByRepo[a/x] = %+v, want 1 call, cost 0.3", got)
	}
}
