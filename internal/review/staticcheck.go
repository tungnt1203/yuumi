package review

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
	cmd := exec.Command("gofmt", "-l", ".")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	files := strings.TrimSpace(string(out))
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
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = dir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err == nil {
		return ""
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		return ""
	}
	return "go vet:\n" + msg
}
