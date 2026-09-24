package gitrepo

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withFakeGit đặt 1 script "git" giả lên đầu PATH: mỗi lần chạy ghi 1 dòng
// "args=<args> | header=<GIT_CONFIG_VALUE_0> | prompt=<GIT_TERMINAL_PROMPT>"
// vào file log rồi thoát 0, để test xem CloneRepo gọi git với args/env nào
// mà không cần mạng hay repo thật. Trả về đường dẫn file log.
func withFakeGit(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake git script is a shell script, skip on windows")
	}

	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\n" +
		`echo "args=$* | header=$GIT_CONFIG_VALUE_0 | prompt=$GIT_TERMINAL_PROMPT" >> "` + logPath + `"` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("cannot write fake git script: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Env của process test không được làm sai kết quả (vd máy dev tự set).
	t.Setenv("GIT_CONFIG_VALUE_0", "")
	return logPath
}

// gitCalls đọc file log của withFakeGit, trả mỗi lệnh git là 1 phần tử.
func gitCalls(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("cannot read fake git log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestCloneRepo_TokenOnlyInFetchEnv(t *testing.T) {
	logPath := withFakeGit(t)
	const token = "ghs_supersecret"

	dir, cleanup, err := CloneRepo("owner/private-repo", "abc123", token)
	if err != nil {
		t.Fatalf("CloneRepo() error = %v", err)
	}
	defer cleanup()
	if dir == "" {
		t.Fatal("CloneRepo() dir is empty")
	}

	calls := gitCalls(t, logPath)
	if len(calls) != 4 {
		t.Fatalf("got %d git calls, want 4 (init, remote add, fetch, checkout): %q", len(calls), calls)
	}

	wantHeader := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	for _, call := range calls {
		args, _, _ := strings.Cut(call, " | ")
		if strings.Contains(args, token) {
			t.Errorf("token leaked into git args: %q", args)
		}

		isFetch := strings.Contains(args, " fetch ")
		hasHeader := strings.Contains(call, "header="+wantHeader+" |")
		if isFetch && !hasHeader {
			t.Errorf("fetch call missing auth header: %q", call)
		}
		if !isFetch && strings.Contains(call, "header=AUTHORIZATION") {
			t.Errorf("non-fetch call got auth header, want only fetch: %q", call)
		}
	}

	// URL remote nằm trong .git/config mà Claude CLI đọc được — không được
	// chứa token.
	if !strings.Contains(calls[1], "remote add origin https://github.com/owner/private-repo.git |") {
		t.Errorf("remote add call = %q, want plain URL without credentials", calls[1])
	}
}

func TestCloneRepo_NoTokenFetchesWithoutAuth(t *testing.T) {
	logPath := withFakeGit(t)

	_, cleanup, err := CloneRepo("owner/public-repo", "abc123", "")
	if err != nil {
		t.Fatalf("CloneRepo() error = %v", err)
	}
	defer cleanup()

	for _, call := range gitCalls(t, logPath) {
		if strings.Contains(call, "header=AUTHORIZATION") {
			t.Errorf("call has auth header without token: %q", call)
		}
		if strings.Contains(call, " fetch ") && !strings.Contains(call, "prompt=0") {
			t.Errorf("fetch call must set GIT_TERMINAL_PROMPT=0: %q", call)
		}
	}
}
