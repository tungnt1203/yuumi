package claudecli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// withFakeClaude tạo 1 script tên "claude" trong thư mục tạm, chèn thư mục
// đó lên đầu PATH cho test hiện tại (t.Setenv tự phục hồi PATH cũ khi test
// xong), để Review() gọi phải bản giả này thay vì Claude CLI thật.
func withFakeClaude(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake claude script is a shell script, skip on windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("cannot write fake claude script: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// noSleep bỏ qua chờ backoff thật — dùng cho mọi test kích hoạt retry, để
// test không tốn vài giây chờ vô ích mỗi lần chạy.
func noSleep(time.Duration) {}

// countingScript trả về 1 script "claude" giả tự đếm số lần được gọi vào
// counterFile (đọc/ghi bằng shell, không cần Go xử lý IPC) — dùng để assert
// Review() gọi Claude CLI đúng bao nhiêu lần. body là phần thân xử lý theo
// từng lần gọi, nhận biến "$count" (số thứ tự lần gọi hiện tại, đếm từ 1).
func countingScript(counterFile, body string) string {
	return fmt.Sprintf(`#!/bin/sh
count=$(( $(cat %q 2>/dev/null || echo 0) + 1 ))
echo "$count" > %q
%s
`, counterFile, counterFile, body)
}

// assertAttempts kiểm tra script giả (countingScript) đã được gọi đúng
// `want` lần.
func assertAttempts(t *testing.T, counterFile string, want int) {
	t.Helper()
	data, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatalf("cannot read attempts counter: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if got != fmt.Sprintf("%d", want) {
		t.Errorf("expected claude to be invoked %d time(s), got %s", want, got)
	}
}

func TestReview_Success(t *testing.T) {
	withFakeClaude(t, `#!/bin/sh
echo '{"type":"result","subtype":"success","is_error":false,"result":"looks good","num_turns":3}'
`)

	got, attempts, numTurns, err := (&Reviewer{}).Review("review this", t.TempDir())
	if err != nil {
		t.Fatalf("Review() unexpected error: %v", err)
	}
	if got != "looks good" {
		t.Errorf("Review() = %q, want %q", got, "looks good")
	}
	if attempts != 1 {
		t.Errorf("Review() attempts = %d, want 1 (no retry needed)", attempts)
	}
	if numTurns != 3 {
		t.Errorf("Review() numTurns = %d, want 3", numTurns)
	}
}

func TestReview_ClaudeReportsError_DoesNotRetry(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "attempts")
	withFakeClaude(t, countingScript(counter, `echo '{"type":"result","subtype":"error_max_turns","is_error":true,"result":"gave up","num_turns":2}'`))

	r := &Reviewer{sleep: noSleep}
	_, attempts, numTurns, err := r.Review("review this", t.TempDir())
	if err == nil {
		t.Fatal("Review() expected error when is_error is true, got nil")
	}
	if !strings.Contains(err.Error(), "gave up") {
		t.Errorf("Review() error = %v, want it to mention %q", err, "gave up")
	}
	// is_error=true là lỗi Claude tự xác định rõ ràng — retry không giúp
	// gì, không nên gọi lại (xem issue #11).
	assertAttempts(t, counter, 1)
	if attempts != 1 {
		t.Errorf("Review() attempts = %d, want 1", attempts)
	}
	// JSON vẫn parse được dù is_error=true — num_turns đọc được bình thường.
	if numTurns != 2 {
		t.Errorf("Review() numTurns = %d, want 2", numTurns)
	}
}

func TestReview_InvalidJSON_DoesNotRetry(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "attempts")
	withFakeClaude(t, countingScript(counter, `echo 'not json'`))

	r := &Reviewer{sleep: noSleep}
	_, attempts, numTurns, err := r.Review("review this", t.TempDir())
	if err == nil {
		t.Fatal("Review() expected error on invalid JSON output, got nil")
	}
	assertAttempts(t, counter, 1)
	if attempts != 1 {
		t.Errorf("Review() attempts = %d, want 1", attempts)
	}
	// Không parse được JSON thì không có gì để đọc num_turns.
	if numTurns != 0 {
		t.Errorf("Review() numTurns = %d, want 0", numTurns)
	}
}

func TestReview_CommandFails_RetriesThenGivesUp(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "attempts")
	withFakeClaude(t, countingScript(counter, `echo 'boom' >&2
exit 1`))

	r := &Reviewer{sleep: noSleep}
	_, attempts, numTurns, err := r.Review("review this", t.TempDir())
	if err == nil {
		t.Fatal("Review() expected error when claude command exits non-zero, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("Review() error = %v, want it to include stderr %q", err, "boom")
	}
	// Lỗi chạy lệnh (exec) là ứng viên retry — mặc định thử đủ
	// defaultMaxAttempts lần rồi mới chịu thua.
	assertAttempts(t, counter, defaultMaxAttempts)
	if attempts != defaultMaxAttempts {
		t.Errorf("Review() attempts = %d, want %d", attempts, defaultMaxAttempts)
	}
	// Lệnh chạy thất bại thì không có output để đọc num_turns.
	if numTurns != 0 {
		t.Errorf("Review() numTurns = %d, want 0", numTurns)
	}
}

func TestReview_CommandFails_RetriesThenSucceeds(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "attempts")
	withFakeClaude(t, countingScript(counter, `if [ "$count" -eq 1 ]; then
  echo 'network blip' >&2
  exit 1
fi
echo '{"type":"result","subtype":"success","is_error":false,"result":"ok after retry","num_turns":4}'`))

	r := &Reviewer{sleep: noSleep}
	got, attempts, numTurns, err := r.Review("review this", t.TempDir())
	if err != nil {
		t.Fatalf("Review() unexpected error after retry: %v", err)
	}
	if got != "ok after retry" {
		t.Errorf("Review() = %q, want %q", got, "ok after retry")
	}
	assertAttempts(t, counter, 2)
	if attempts != 2 {
		t.Errorf("Review() attempts = %d, want 2", attempts)
	}
	if numTurns != 4 {
		t.Errorf("Review() numTurns = %d, want 4", numTurns)
	}
}

func TestReview_MaxAttempts_Override(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "attempts")
	withFakeClaude(t, countingScript(counter, `echo 'always fails' >&2
exit 1`))

	r := &Reviewer{MaxAttempts: 2, sleep: noSleep}
	_, attempts, _, err := r.Review("review this", t.TempDir())
	if err == nil {
		t.Fatal("Review() expected error after exhausting retries, got nil")
	}
	assertAttempts(t, counter, 2)
	if attempts != 2 {
		t.Errorf("Review() attempts = %d, want 2", attempts)
	}
}

func TestReview_PassesPromptAndDir(t *testing.T) {
	// Script tự kiểm tra $CLAUDE_TEST_DIR (cwd) và arg -p, in kết quả tương ứng
	// để test khỏi phải parse lại argv trong Go.
	withFakeClaude(t, `#!/bin/sh
if [ "$1" = "-p" ] && [ "$2" = "hello prompt" ]; then
  echo '{"type":"result","subtype":"success","is_error":false,"result":"got-prompt"}'
else
  echo '{"type":"result","subtype":"success","is_error":false,"result":"wrong-args"}'
fi
`)

	dir := t.TempDir()
	got, _, _, err := (&Reviewer{}).Review("hello prompt", dir)
	if err != nil {
		t.Fatalf("Review() unexpected error: %v", err)
	}
	if got != "got-prompt" {
		t.Errorf("Review() = %q, want claude to receive the prompt via -p", got)
	}
}
