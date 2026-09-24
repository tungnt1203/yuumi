package review

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeGoModule dựng 1 Go module tối thiểu trong thư mục tạm: go.mod +
// 1 file .go với nội dung tuỳ test — đủ để gofmt/go vet chạy thật trên đó.
func writeGoModule(t *testing.T, goFileContent string) string {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module statictest\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("cannot write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goFileContent), 0o644); err != nil {
		t.Fatalf("cannot write main.go: %v", err)
	}

	return dir
}

func TestStaticCheckReport_NotGoRepo_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir() // không có go.mod

	got := staticCheckReport(dir)
	if got != "" {
		t.Errorf("staticCheckReport() = %q, want empty (not a Go repo)", got)
	}
}

func TestStaticCheckReport_CleanRepo_ReturnsEmpty(t *testing.T) {
	dir := writeGoModule(t, `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)

	got := staticCheckReport(dir)
	if got != "" {
		t.Errorf("staticCheckReport() = %q, want empty (gofmt/go vet clean)", got)
	}
}

func TestStaticCheckReport_DetectsGofmtIssue(t *testing.T) {
	// "x:=1" thiếu khoảng trắng quanh ":=" — hợp lệ cú pháp nhưng gofmt sẽ
	// format lại, nên gofmt -l phải liệt kê file này.
	dir := writeGoModule(t, `package main

func main() {
	x:=1
	_ = x
}
`)

	got := staticCheckReport(dir)
	if !strings.Contains(got, "gofmt") {
		t.Errorf("staticCheckReport() = %q, want it to mention gofmt", got)
	}
	if !strings.Contains(got, "main.go") {
		t.Errorf("staticCheckReport() = %q, want it to name main.go", got)
	}
}

func TestStaticCheckReport_DetectsVetIssue(t *testing.T) {
	// Printf format "%d" với arg string — go vet luôn bắt được lỗi kinh
	// điển này.
	dir := writeGoModule(t, `package main

import "fmt"

func main() {
	fmt.Printf("%d\n", "not a number")
}
`)

	got := staticCheckReport(dir)
	if !strings.Contains(got, "go vet") {
		t.Errorf("staticCheckReport() = %q, want it to mention go vet", got)
	}
}

func TestStaticCheckReport_IncludedInPrompt(t *testing.T) {
	dir := writeGoModule(t, `package main

func main() {
	x:=1
	_ = x
}
`)

	report := staticCheckReport(dir)
	if report == "" {
		t.Fatal("expected non-empty static check report as precondition")
	}

	prompt := BuildReviewPrompt("review", "diff --git a/main.go b/main.go\n+x", report, "", "")
	if !strings.Contains(prompt, "gofmt") {
		t.Errorf("expected prompt to include static check note, got:\n%s", prompt)
	}
}

// withFakeGoTools đặt script "go" và "gofmt" giả lên đầu PATH. PATH chỉ còn
// thư mục giả + /bin:/usr/bin (cho sleep/env), để chắc chắn không gọi nhầm
// toolchain thật.
func withFakeGoTools(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake go script is a shell script, skip on windows")
	}
	bin := t.TempDir()
	for _, name := range []string{"go", "gofmt"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/bin:/usr/bin")
}

// Lệnh treo bị cắt theo staticCheckTimeout, không giữ job vô hạn (#78).
func TestStaticCheckReport_Timeout(t *testing.T) {
	withFakeGoTools(t, "#!/bin/sh\nsleep 30\n")
	old := staticCheckTimeout
	staticCheckTimeout = 200 * time.Millisecond
	t.Cleanup(func() { staticCheckTimeout = old })

	start := time.Now()
	got := staticCheckReport(writeGoModule(t, "package main\n"))

	if got != "" {
		t.Errorf("staticCheckReport() = %q, want empty on timeout", got)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("staticCheckReport() took %v, want it cut by the timeout", elapsed)
	}
}

// go vet chạy với env đã siết: không có secret của server, có các biến
// chặn tải toolchain/cgo/VCS (#78).
func TestStaticCheckReport_HardenedEnv(t *testing.T) {
	// "go vet" giả in env ra stderr rồi exit 1, để env hiện trong report.
	withFakeGoTools(t, "#!/bin/sh\n[ \"$1\" = vet ] || exit 0\nenv >&2\nexit 1\n")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "top-secret")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "pem")

	got := staticCheckReport(writeGoModule(t, "package main\n"))

	if strings.Contains(got, "top-secret") || strings.Contains(got, "GITHUB_APP_PRIVATE_KEY") {
		t.Errorf("go vet env leaks server secrets:\n%s", got)
	}
	for _, want := range []string{"GOTOOLCHAIN=local", "CGO_ENABLED=0", "GOPROXY=https://proxy.golang.org", "GOFLAGS=-mod=readonly"} {
		if !strings.Contains(got, want) {
			t.Errorf("go vet env missing %s, got:\n%s", want, got)
		}
	}
}
