package review

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tungnt1203/yuumi/internal/sandbox"
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

	got := staticCheckReport(sandbox.Local(dir))
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

	got := staticCheckReport(sandbox.Local(dir))
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

	got := staticCheckReport(sandbox.Local(dir))
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

	got := staticCheckReport(sandbox.Local(dir))
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

	report := staticCheckReport(sandbox.Local(dir))
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
	got := staticCheckReport(sandbox.Local(writeGoModule(t, "package main\n")))

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
	// "go vet" giả in env ra stderr dưới dạng dòng chẩn đoán rồi exit 1,
	// để env hiện trong report.
	withFakeGoTools(t, "#!/bin/sh\n[ \"$1\" = vet ] || exit 0\nenv | sed 's/^/x.go:1:1: /' >&2\nexit 1\n")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "top-secret")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "pem")

	got := staticCheckReport(sandbox.Local(writeGoModule(t, "package main\n")))

	if strings.Contains(got, "top-secret") || strings.Contains(got, "GITHUB_APP_PRIVATE_KEY") {
		t.Errorf("go vet env leaks server secrets:\n%s", got)
	}
	for _, want := range []string{"GOTOOLCHAIN=local", "CGO_ENABLED=0", "GOPROXY=https://proxy.golang.org", "GOFLAGS=-mod=readonly"} {
		if !strings.Contains(got, want) {
			t.Errorf("go vet env missing %s, got:\n%s", want, got)
		}
	}
}

// Hết timeout thì kill cả process group: tiến trình con go vet sinh ra
// không được chạy tiếp thành mồ côi (#78). Timeout để tới vài giây vì
// macOS có thể quét script mới ghi hàng trăm ms trước khi cho chạy — timeout
// quá ngắn thì script bị kill trước khi kịp sinh tiến trình con, test pass
// mà không kiểm tra được gì (marker "started" chặn trường hợp đó).
func TestStaticCheckReport_TimeoutKillsChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group kill is unix-only")
	}
	d := t.TempDir()
	started, survived := filepath.Join(d, "started"), filepath.Join(d, "survived")
	// Tiến trình con chạy nền, 3 giây sau (quá timeout 2s) ghi marker nếu
	// còn sống.
	withFakeGoTools(t, fmt.Sprintf("#!/bin/sh\ntouch %q\n(sleep 3; touch %q) &\nsleep 30\n", started, survived))
	old := staticCheckTimeout
	staticCheckTimeout = 2 * time.Second
	t.Cleanup(func() { staticCheckTimeout = old })

	gofmtReport(sandbox.Local(writeGoModule(t, "package main\n")))
	time.Sleep(2 * time.Second)

	if _, err := os.Stat(started); err != nil {
		t.Fatal("fake gofmt never started before the timeout, test proves nothing")
	}
	if _, err := os.Stat(survived); err == nil {
		t.Error("child process survived the timeout, want the whole process group killed")
	}
}

// Lỗi môi trường không có vị trí file (tải module, cgo tắt...) không được
// đưa vào prompt như cảnh báo vet; dòng chẩn đoán thật thì giữ.
func TestVetDiagnostics(t *testing.T) {
	setup := "go: example.com/private@v1.0.0: reading https://proxy.golang.org/...: 404 Not Found\n" +
		"package x: build constraints exclude all Go files in /tmp/x\n"
	if got := vetDiagnostics(setup); got != "" {
		t.Errorf("vetDiagnostics(setup errors) = %q, want empty", got)
	}

	diag := "# statictest\n./main.go:6:2: fmt.Printf format %d has arg of wrong type\n"
	if got := vetDiagnostics(diag); got != "./main.go:6:2: fmt.Printf format %d has arg of wrong type" {
		t.Errorf("vetDiagnostics(diag) = %q, want only the diagnostic line", got)
	}

	// Output thật của go 1.26 cho lỗi type-check: dòng have/want thụt tab
	// là phần tiếp theo của chẩn đoán.
	typeErr := "# a\n# [a]\nvet: ./main.go:9:4: not enough arguments in call to f\n\thave ()\n\twant (int)\n"
	want := "vet: ./main.go:9:4: not enough arguments in call to f\n\thave ()\n\twant (int)"
	if got := vetDiagnostics(typeErr); got != want {
		t.Errorf("vetDiagnostics(typeErr) = %q, want %q", got, want)
	}

	// go.mod hỏng do PR là lỗi của PR, không phải lỗi môi trường.
	goModErr := "go: errors parsing go.mod:\ngo.mod:4: unknown directive: foo\n"
	if got := vetDiagnostics(goModErr); got != "go.mod:4: unknown directive: foo" {
		t.Errorf("vetDiagnostics(goModErr) = %q, want the go.mod line", got)
	}
}
