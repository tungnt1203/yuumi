package githubapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// installationTokenHTTPTimeout giới hạn thời gian chờ gọi
// POST /app/installations/{id}/access_tokens — http.DefaultClient mặc định
// KHÔNG có timeout, nên nếu GitHub API treo, request sẽ block vô thời hạn.
// Provider.Token() gọi hàm này trong lúc giữ mutex cache (xem provider.go),
// nên 1 request treo mãi sẽ chặn theo mọi installation khác đang cần đọc
// cache — cần timeout rõ ràng để tránh deadlock/goroutine leak thực tế dưới
// tải đồng thời (phát hiện qua yuumi-review tự review PR #47).
const installationTokenHTTPTimeout = 10 * time.Second

// InstallationToken là token GitHub cấp cho 1 installation cụ thể (App đã
// cài vào repo nào) — token này mới gọi được API thật trên repo đó (post
// comment, review...), khác với App-level JWT (xem GenerateAppJWT) chỉ dùng
// để xin đúng token này.
type InstallationToken struct {
	Token     string
	ExpiresAt time.Time
}

// apiBaseURL trỏ tới GitHub API thật; test đổi biến này sang httptest.Server
// để kiểm tra logic đổi token (parse response, xử lý lỗi HTTP) mà không gọi
// mạng thật.
var apiBaseURL = "https://api.github.com"

// tokenPermissions là đúng các quyền bot cần, gửi kèm khi xin installation
// token để token chỉ có ngần đó quyền, dù App được cấp rộng hơn — token lộ
// (vd qua tiến trình con chạy trên code PR) cũng không sửa được code,
// workflow hay secret của repo. Thêm endpoint GitHub mới thì thêm quyền
// tương ứng ở đây, nếu không API trả 403.
//
//   - contents: read       — clone repo (gitrepo.CloneRepo), GET /compare
//   - issues: write        — post/sửa comment, reaction trên comment PR
//   - pull_requests: write — GET /pulls, tạo review inline (POST /reviews)
//   - checks: write        — tạo/cập nhật check run "yuumi review" (#59)
//
// metadata: read GitHub luôn tự cấp. Xin quyền mà App không có thì GitHub
// trả 422 — lỗi ngay lúc xin token, không âm thầm chạy với quyền thiếu.
var tokenPermissions = map[string]string{
	"contents":      "read",
	"issues":        "write",
	"pull_requests": "write",
	"checks":        "write",
}

// installationTokenResponse map đúng field GitHub trả về từ
// POST /app/installations/{id}/access_tokens — chỉ lấy 2 field cần dùng.
type installationTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// GetInstallationToken đổi 1 App-level JWT (xem GenerateAppJWT) lấy
// installation access token cho đúng installationID — mỗi lần gọi ra 1 token
// mới, sống khoảng 1h (GitHub tự quyết định, đọc từ ExpiresAt trả về, không
// hardcode 1h). Việc cache/tự làm mới token này để khỏi gọi lại API mỗi lần
// xử lý webhook là việc của bước sau (xem issue #47), hàm này chỉ lo đúng 1
// việc: đổi JWT lấy 1 token.
//
// ctx nhận từ request webhook gọi xuống (xem Provider.Token), để có thể huỷ
// theo đúng vòng đời request đó, cộng với timeout riêng
// (installationTokenHTTPTimeout) phòng trường hợp ctx không có deadline.
func GetInstallationToken(ctx context.Context, appJWT string, installationID int64) (InstallationToken, error) {
	ctx, cancel := context.WithTimeout(ctx, installationTokenHTTPTimeout)
	defer cancel()

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", apiBaseURL, installationID)

	reqBody, err := json.Marshal(map[string]any{"permissions": tokenPermissions})
	if err != nil {
		return InstallationToken{}, fmt.Errorf("cannot encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return InstallationToken{}, fmt.Errorf("cannot create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return InstallationToken{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return InstallationToken{}, fmt.Errorf("cannot read response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return InstallationToken{}, fmt.Errorf("github api error %d: %s", resp.StatusCode, string(body))
	}

	var parsed installationTokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return InstallationToken{}, fmt.Errorf("cannot decode response: %w", err)
	}

	return InstallationToken{Token: parsed.Token, ExpiresAt: parsed.ExpiresAt}, nil
}
