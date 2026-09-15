package claudecli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

func TestReview_Success(t *testing.T) {
	withFakeClaude(t, `#!/bin/sh
echo '{"type":"result","subtype":"success","is_error":false,"result":"looks good"}'
`)

	got, err := (&Reviewer{}).Review("review this", t.TempDir())
	if err != nil {
		t.Fatalf("Review() unexpected error: %v", err)
	}
	if got != "looks good" {
		t.Errorf("Review() = %q, want %q", got, "looks good")
	}
}

func TestReview_ClaudeReportsError(t *testing.T) {
	withFakeClaude(t, `#!/bin/sh
echo '{"type":"result","subtype":"error_max_turns","is_error":true,"result":"gave up"}'
`)

	_, err := (&Reviewer{}).Review("review this", t.TempDir())
	if err == nil {
		t.Fatal("Review() expected error when is_error is true, got nil")
	}
	if !strings.Contains(err.Error(), "gave up") {
		t.Errorf("Review() error = %v, want it to mention %q", err, "gave up")
	}
}

func TestReview_InvalidJSON(t *testing.T) {
	withFakeClaude(t, `#!/bin/sh
echo 'not json'
`)

	_, err := (&Reviewer{}).Review("review this", t.TempDir())
	if err == nil {
		t.Fatal("Review() expected error on invalid JSON output, got nil")
	}
}

func TestReview_CommandFails(t *testing.T) {
	withFakeClaude(t, `#!/bin/sh
echo 'boom' >&2
exit 1
`)

	_, err := (&Reviewer{}).Review("review this", t.TempDir())
	if err == nil {
		t.Fatal("Review() expected error when claude command exits non-zero, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("Review() error = %v, want it to include stderr %q", err, "boom")
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
	got, err := (&Reviewer{}).Review("hello prompt", dir)
	if err != nil {
		t.Fatalf("Review() unexpected error: %v", err)
	}
	if got != "got-prompt" {
		t.Errorf("Review() = %q, want claude to receive the prompt via -p", got)
	}
}
