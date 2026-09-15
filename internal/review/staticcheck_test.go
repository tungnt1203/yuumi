package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
