package egress

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
)

// Biến môi trường chứa credential Claude — cùng tên Claude CLI đọc.
const (
	OAuthTokenEnv = "CLAUDE_CODE_OAUTH_TOKEN"
	APIKeyEnv     = "ANTHROPIC_API_KEY"
)

// Credential là credential thật để gọi Anthropic API. Chỉ proxy giữ nó;
// sandbox chỉ có giá trị giả cùng loại (issue #78, bước 3).
type Credential struct {
	// Env là tên biến chứa credential (OAuthTokenEnv hoặc APIKeyEnv) —
	// quyết định header gắn vào request.
	Env   string
	Value string
}

// CredentialFromEnv đọc credential từ env, ưu tiên OAuth token (giống Claude
// CLI). Không có cái nào thì trả lỗi.
func CredentialFromEnv(getenv func(string) string) (Credential, error) {
	for _, name := range []string{OAuthTokenEnv, APIKeyEnv} {
		if v := getenv(name); v != "" {
			return Credential{Env: name, Value: v}, nil
		}
	}
	return Credential{}, fmt.Errorf("thiếu credential Claude: set %s hoặc %s", OAuthTokenEnv, APIKeyEnv)
}

// anthropicAPI là đích duy nhất của credential proxy.
var anthropicAPI = &url.URL{Scheme: "https", Host: "api.anthropic.com"}

// NewCredentialProxy tạo reverse proxy tới Anthropic API: sandbox gọi nó
// qua ANTHROPIC_BASE_URL bằng HTTP thường (trong network nội bộ) với
// credential GIẢ; proxy bỏ mọi header xác thực của request, gắn credential
// thật rồi chuyển tiếp qua HTTPS. Code PR trong sandbox (kể cả khi dụ được
// Claude đọc env) không lấy được credential thật.
//
// Chỉ chuyển tiếp đường dẫn /v1/ (Claude CLI chỉ gọi POST /v1/messages —
// đã kiểm chứng bằng log khi chạy claude -p thật với credential giả).
func NewCredentialProxy(cred Credential, logf func(format string, args ...any)) http.Handler {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(anthropicAPI)
			// Header xác thực từ sandbox chỉ là giá trị giả: luôn bỏ, kể cả
			// loại không dùng, để không có đường nào đi tiếp tới Anthropic.
			r.Out.Header.Del("Authorization")
			r.Out.Header.Del("X-Api-Key")
			switch cred.Env {
			case OAuthTokenEnv:
				r.Out.Header.Set("Authorization", "Bearer "+cred.Value)
			default:
				r.Out.Header.Set("X-Api-Key", cred.Value)
			}
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path phải đã chuẩn hoá: "/v1/../x" có tiền tố /v1/ nhưng trỏ ra
		// ngoài /v1/.
		if !strings.HasPrefix(r.URL.Path, "/v1/") || path.Clean(r.URL.Path) != r.URL.Path {
			logf("credential DENY %s %s", r.Method, r.URL.Path)
			http.Error(w, "path not allowed", http.StatusForbidden)
			return
		}
		logf("credential ALLOW %s %s", r.Method, r.URL.Path)
		rp.ServeHTTP(w, r)
	})
}

// CredentialFromOSEnv là CredentialFromEnv đọc env thật của process.
func CredentialFromOSEnv() (Credential, error) {
	return CredentialFromEnv(os.Getenv)
}
