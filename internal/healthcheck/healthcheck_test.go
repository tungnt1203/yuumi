package healthcheck

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// withFakeClaude tạo 1 script tên "claude" trong thư mục tạm và chèn lên
// đầu PATH cho test hiện tại — cùng cách claudecli/claude_test.go giả lập
// CLI thật, để DefaultClaudeCLICheck không phụ thuộc máy chạy test có cài
// claude hay không.
func withFakeClaude(t *testing.T, exitCode int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake claude script is a shell script, skip on windows")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("cannot write fake claude script: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestMonitor_Check_AllOK(t *testing.T) {
	m := &Monitor{
		CheckClaudeCLI: func() error { return nil },
		CheckGitHubApp: func() error { return nil },
	}

	report := m.Check()

	if !report.Healthy() {
		t.Fatalf("Healthy() = false, want true; report = %+v", report)
	}
	if !report.ClaudeCLI.OK || report.ClaudeCLI.Message != "ok" {
		t.Errorf("ClaudeCLI = %+v, want OK with message %q", report.ClaudeCLI, "ok")
	}
	if !report.GitHubApp.OK || report.GitHubApp.Message != "ok" {
		t.Errorf("GitHubApp = %+v, want OK with message %q", report.GitHubApp, "ok")
	}
	if report.CheckedAt.IsZero() {
		t.Error("CheckedAt is zero, want set")
	}
}

func TestMonitor_Check_ClaudeCLIFails(t *testing.T) {
	wantErr := errors.New("claude: command not found")
	m := &Monitor{
		CheckClaudeCLI: func() error { return wantErr },
		CheckGitHubApp: func() error { return nil },
	}

	report := m.Check()

	if report.Healthy() {
		t.Fatal("Healthy() = true, want false when claude CLI check fails")
	}
	if report.ClaudeCLI.OK {
		t.Error("ClaudeCLI.OK = true, want false")
	}
	if report.ClaudeCLI.Message != wantErr.Error() {
		t.Errorf("ClaudeCLI.Message = %q, want %q", report.ClaudeCLI.Message, wantErr.Error())
	}
	// GitHub App vẫn phải được check độc lập, không bị bỏ qua vì claude CLI
	// đã fail trước đó.
	if !report.GitHubApp.OK {
		t.Error("GitHubApp.OK = false, want true (must not short-circuit)")
	}
}

func TestMonitor_Check_GitHubAppFails(t *testing.T) {
	wantErr := errors.New("xác thực GitHub App thất bại")
	m := &Monitor{
		CheckClaudeCLI: func() error { return nil },
		CheckGitHubApp: func() error { return wantErr },
	}

	report := m.Check()

	if report.Healthy() {
		t.Fatal("Healthy() = true, want false when GitHub App check fails")
	}
	if report.GitHubApp.OK {
		t.Error("GitHubApp.OK = true, want false")
	}
	if report.GitHubApp.Message != wantErr.Error() {
		t.Errorf("GitHubApp.Message = %q, want %q", report.GitHubApp.Message, wantErr.Error())
	}
}

func TestMonitor_Last_ReturnsCachedResultWithoutRechecking(t *testing.T) {
	calls := 0
	m := &Monitor{
		CheckClaudeCLI: func() error { calls++; return nil },
		CheckGitHubApp: func() error { return nil },
	}

	// Chưa Check() lần nào: Last() phải trả zero-value, không tự chạy check.
	if got := m.Last(); got.Healthy() {
		t.Errorf("Last() before any Check() = %+v, want zero-value (not healthy)", got)
	}
	if calls != 0 {
		t.Fatalf("CheckClaudeCLI called %d times before Check(), want 0", calls)
	}

	want := m.Check()
	if calls != 1 {
		t.Fatalf("CheckClaudeCLI called %d times after 1 Check(), want 1", calls)
	}

	got := m.Last()
	got.CheckedAt = want.CheckedAt // so sánh phần còn lại, tránh lệ thuộc time.Now()
	if got != want {
		t.Errorf("Last() = %+v, want %+v", got, want)
	}
	if calls != 1 {
		t.Errorf("CheckClaudeCLI called %d times after Last(), want 1 (Last must not recheck)", calls)
	}
}

func TestNewMonitor_UsesDefaultChecks(t *testing.T) {
	m := NewMonitor("some-app-id", []byte("some-key"))
	if m.CheckClaudeCLI == nil {
		t.Error("CheckClaudeCLI is nil, want DefaultClaudeCLICheck")
	}
	if m.CheckGitHubApp == nil {
		t.Error("CheckGitHubApp is nil, want a check wrapping DefaultGitHubAppCheck")
	}
}

func TestDefaultClaudeCLICheck(t *testing.T) {
	t.Run("claude available", func(t *testing.T) {
		withFakeClaude(t, 0)
		if err := DefaultClaudeCLICheck(); err != nil {
			t.Errorf("DefaultClaudeCLICheck() = %v, want nil", err)
		}
	})

	t.Run("claude exits with error", func(t *testing.T) {
		withFakeClaude(t, 1)
		if err := DefaultClaudeCLICheck(); err == nil {
			t.Error("DefaultClaudeCLICheck() = nil, want error")
		}
	})

	t.Run("claude not on PATH", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if err := DefaultClaudeCLICheck(); err == nil {
			t.Error("DefaultClaudeCLICheck() = nil, want error")
		}
	})
}

func TestCheckBearerTokenAgainst(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "valid token", statusCode: http.StatusOK, wantErr: false},
		{name: "invalid or revoked token", statusCode: http.StatusUnauthorized, wantErr: true},
		{name: "unexpected server error", statusCode: http.StatusInternalServerError, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			err := checkBearerTokenAgainst(server.URL, "fake-token")
			if (err != nil) != tt.wantErr {
				t.Errorf("checkBearerTokenAgainst() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultGitHubAppCheck_InvalidPrivateKey(t *testing.T) {
	// Không cần gọi API thật: private key sai phải fail ngay ở bước ký JWT.
	err := DefaultGitHubAppCheck("app-id", []byte("not a valid pem"))
	if err == nil {
		t.Error("DefaultGitHubAppCheck() with invalid private key: expected error, got nil")
	}
}
