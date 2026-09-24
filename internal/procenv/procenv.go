// Package procenv dựng biến môi trường cho các tiến trình con chạy trên
// code của PR (Claude CLI, go vet...). Code PR là input không tin cậy: tiến
// trình con không được thừa hưởng secret của server qua env (issue #78).
package procenv

import "strings"

// secretNames là các biến chứa secret của server (xem internal/config).
// Không tiến trình con nào chạy trên code PR cần tới chúng.
var secretNames = []string{
	"GITHUB_WEBHOOK_SECRET",
	"GITHUB_APP_PRIVATE_KEY",
	"GITHUB_APP_PRIVATE_KEY_PATH",
	"GITHUB_TOKEN",
	"GH_TOKEN",
}

// WithoutSecrets trả bản sao env (dạng "KEY=value" như os.Environ) đã bỏ
// các biến secret của server. Dùng khi tiến trình con cần phần lớn env của
// server (vd Claude CLI cần biến xác thực, proxy... không liệt kê trước
// được hết).
func WithoutSecrets(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !hasName(kv, secretNames) {
			out = append(out, kv)
		}
	}
	return out
}

// Only trả bản sao env chỉ giữ các biến có tên trong names. Dùng khi biết
// chắc tiến trình con cần gì (vd go vet), an toàn hơn WithoutSecrets vì biến
// mới thêm vào server sau này không tự lọt vào.
func Only(env []string, names ...string) []string {
	var out []string
	for _, kv := range env {
		if hasName(kv, names) {
			out = append(out, kv)
		}
	}
	return out
}

func hasName(kv string, names []string) bool {
	name, _, _ := strings.Cut(kv, "=")
	for _, n := range names {
		if name == n {
			return true
		}
	}
	return false
}
