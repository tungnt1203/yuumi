package review

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tungnt1203/yuumi/internal/procenv"
	"github.com/tungnt1203/yuumi/internal/sandbox"
)

// vetDiagnosticRe khớp dòng chẩn đoán gắn với vị trí trong file của PR
// ("./main.go:12:3: ...", "go.mod:4: unknown directive: foo"...) — cảnh
// báo vet, lỗi compile hoặc go.mod/go.work/assembly hỏng do chính PR.
var vetDiagnosticRe = regexp.MustCompile(`(\.go|go\.mod|go\.work|\.s):\d+(:\d+)?: `)

// vetDiagnostics chỉ giữ dòng chẩn đoán có vị trí trong code. Lỗi môi
// trường (tải module private không có trên proxy, package cần cgo khi
// CGO_ENABLED=0, toolchain local cũ hơn go.mod...) không có vị trí file,
// không phải lỗi của PR — đưa vào prompt kèm câu "không cần lặp lại" sẽ cho
// Claude context sai.
//
// Dòng thụt tab ngay sau 1 chẩn đoán là phần tiếp theo của nó (vd
// "\thave ()" / "\twant (int)" của lỗi type-check), giữ lại để Claude
// thấy đủ thông điệp.
func vetDiagnostics(stderr string) string {
	var lines []string
	inDiag := false
	for _, l := range strings.Split(stderr, "\n") {
		switch {
		case vetDiagnosticRe.MatchString(l):
			inDiag = true
			lines = append(lines, strings.TrimSpace(l))
		case inDiag && strings.HasPrefix(l, "\t"):
			lines = append(lines, l)
		default:
			inDiag = false
		}
	}
	return strings.Join(lines, "\n")
}

// staticCheckTimeout giới hạn mỗi lệnh check tĩnh. go vet compile code của
// PR (không tin cậy): build treo hay package cực lớn không được giữ job —
// và slot của dispatcher — vô hạn (issue #78). var để test rút ngắn.
var staticCheckTimeout = 2 * time.Minute

// goEnvKeep là các biến của server mà toolchain Go cần: tìm binary, thư mục
// cache/module, proxy mạng. Mọi biến khác (kể cả secret) bị bỏ.
var goEnvKeep = []string{
	"PATH", "HOME", "TMPDIR",
	"GOROOT", "GOPATH", "GOCACHE", "GOMODCACHE",
	// Server chạy dưới systemd/container có thể không có HOME, Go tìm
	// cache/config qua XDG.
	"XDG_CACHE_HOME", "XDG_CONFIG_HOME",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR",
	// Windows
	"SYSTEMROOT", "USERPROFILE", "LOCALAPPDATA", "APPDATA",
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

// runStaticCheck chạy 1 lệnh check tĩnh trong sandbox box với timeout và
// env đã siết. Hết timeout thì kill cả process group của lệnh phía server
// (killProcessGroupOnCancel): với sandbox.Local đó chính là gofmt/go vet;
// với sandbox Docker chỉ là `docker exec`, tiến trình trong container còn
// chạy tới khi job xoá container (vẫn bị giới hạn CPU/RAM/pids). WaitDelay
// là lưới an toàn đóng pipe nếu vẫn còn tiến trình giữ stdout/stderr.
// timedOut=true thì output không đầy đủ, caller không được coi là kết quả.
func runStaticCheck(box sandbox.Env, name string, args ...string) (stdout, stderr string, timedOut bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), staticCheckTimeout)
	defer cancel()

	cmd := box.Command(ctx, goHardenedEnv(), name, args...)
	killProcessGroupOnCancel(cmd)
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
func staticCheckReport(box sandbox.Env) string {
	if !isGoRepo(box.Dir()) {
		return ""
	}

	var sections []string
	if s := gofmtReport(box); s != "" {
		sections = append(sections, s)
	}
	if s := goVetReport(box); s != "" {
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
func gofmtReport(box sandbox.Env) string {
	out, _, timedOut, err := runStaticCheck(box, "gofmt", "-l", ".")
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
func goVetReport(box sandbox.Env) string {
	_, stderr, timedOut, err := runStaticCheck(box, "go", "vet", "./...")
	if err == nil || timedOut {
		return ""
	}
	msg := vetDiagnostics(stderr)
	if msg == "" {
		if s := strings.TrimSpace(stderr); s != "" {
			fmt.Println("go vet lỗi môi trường, không đưa vào prompt:", s)
		}
		return ""
	}
	return "go vet:\n" + msg
}
