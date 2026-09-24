package review

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tungnt1203/yuumi/internal/procenv"
)

// staticCheckTimeout giới hạn mỗi lệnh check tĩnh. go vet compile code của
// PR (không tin cậy): build treo hay package cực lớn không được giữ job —
// và slot của dispatcher — vô hạn (issue #78). var để test rút ngắn.
var staticCheckTimeout = 2 * time.Minute

// goEnvKeep là các biến của server mà toolchain Go cần: tìm binary, thư mục
// cache/module, proxy mạng. Mọi biến khác (kể cả secret) bị bỏ.
var goEnvKeep = []string{
	"PATH", "HOME", "TMPDIR",
	"GOPATH", "GOCACHE", "GOMODCACHE",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR",
}

// goHardenedEnv là env cho gofmt/go vet trên code PR:
//   - GOTOOLCHAIN=local: không tự tải và chạy Go toolchain khác mà go.mod
//     của PR yêu cầu.
//   - CGO_ENABLED=0: không gọi C compiler với cờ #cgo do PR viết.
//   - GOPROXY chỉ proxy.golang.org, không ",direct": không clone VCS tuỳ ý.
//   - GOFLAGS=-mod=readonly: không sửa go.mod/go.sum.
func goHardenedEnv() []string {
	return append(procenv.Only(os.Environ(), goEnvKeep...),
		"GOTOOLCHAIN=local",
		"CGO_ENABLED=0",
		"GOPROXY=https://proxy.golang.org",
		"GOFLAGS=-mod=readonly",
	)
}

// runStaticCheck chạy 1 lệnh check tĩnh trong dir với timeout và env đã
// siết. WaitDelay đóng pipe nếu tiến trình con của go (compile, vet tool)
// còn giữ stdout/stderr sau khi lệnh chính bị kill. timedOut=true thì
// output không đầy đủ, caller không được coi là kết quả.
func runStaticCheck(dir string, name string, args ...string) (stdout, stderr string, timedOut bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), staticCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = goHardenedEnv()
	cmd.WaitDelay = 5 * time.Second

	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		fmt.Printf("Static check %s quá %v, bỏ qua\n", name, staticCheckTimeout)
		return "", "", true, err
	}
	return out.String(), errOut.String(), false, err
}

// staticCheckReport chạy các check tĩnh có sẵn của toolchain (hiện tại chỉ
// Go: gofmt/go vet) trên repo đã checkout tại dir, trả về báo cáo dạng text
// để chèn vào prompt làm context có sẵn cho Claude — lỗi máy tự bắt được
// (format sai, vet warning) không cần Claude phải tự đọc code ra mới thấy,
// nhờ đó Claude tập trung nhận xét logic/thiết kế thay vì lặp lại việc máy
// đã làm tốt hơn (xem issue #8).
//
// Trả "" nếu không detect được toolchain Go (không có go.mod) hoặc check
// tĩnh không phát hiện vấn đề gì — không cần chèn thêm gì vào prompt.
//
// Issue #8 có thảo luận hỗ trợ nhiều ngôn ngữ (mỗi ngôn ngữ 1 tool format
// riêng) — chưa làm ở đây, chỉ Go vì đây là ngôn ngữ của chính project.
// Repo target không phải Go thì staticCheckReport tự động no-op (isGoRepo
// false), không hại gì; hỗ trợ ngôn ngữ khác là việc mở rộng sau, làm khi
// có repo thật cần.
func staticCheckReport(dir string) string {
	if !isGoRepo(dir) {
		return ""
	}

	var sections []string
	if s := gofmtReport(dir); s != "" {
		sections = append(sections, s)
	}
	if s := goVetReport(dir); s != "" {
		sections = append(sections, s)
	}
	if len(sections) == 0 {
		return ""
	}

	return "Check tĩnh tự động (gofmt/go vet) đã chạy trên code sau khi đổi, kết quả dưới đây — " +
		"KHÔNG cần lặp lại các lỗi này trong review, tập trung nhận xét logic/thiết kế:\n\n" +
		strings.Join(sections, "\n\n")
}

// isGoRepo báo dir có phải Go module không (có go.mod ở root) — proxy đơn
// giản, đủ dùng: repo build được bằng go luôn có go.mod.
func isGoRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}

// gofmtReport chạy "gofmt -l ." trong dir, trả về danh sách file chưa
// format đúng chuẩn (gofmt -l tự in mỗi file 1 dòng ra stdout, exit 0 kể cả
// khi có file chưa format — khác go vet). Lỗi chạy lệnh (gofmt không có
// trên PATH...) bị bỏ qua thay vì chặn review: static check là tiện ích
// thêm, không phải điều kiện bắt buộc để review chạy được.
func gofmtReport(dir string) string {
	out, _, timedOut, err := runStaticCheck(dir, "gofmt", "-l", ".")
	if err != nil || timedOut {
		return ""
	}
	files := strings.TrimSpace(out)
	if files == "" {
		return ""
	}
	return "gofmt (file chưa format đúng chuẩn):\n" + files
}

// goVetReport chạy "go vet ./..." trong dir. go vet in cảnh báo ra stderr
// và exit non-zero khi tìm thấy vấn đề — đây chính là nội dung cần đưa vào
// prompt. err xảy ra nhưng stderr rỗng (vd binary "go" không có trên PATH,
// hoặc package không compile được vì lý do khác vet) thì không có gì đáng
// tin cậy để báo cáo, bỏ qua.
func goVetReport(dir string) string {
	_, stderr, timedOut, err := runStaticCheck(dir, "go", "vet", "./...")
	if err == nil || timedOut {
		return ""
	}
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		return ""
	}
	return "go vet:\n" + msg
}
