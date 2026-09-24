// Package healthcheck xác thực các dependency ngoài mà bot cần để review
// (Claude CLI, GitHub App auth) thật sự dùng được, thay vì chỉ tin biến môi
// trường đã set là đủ (xem issue #29: /health trước đây luôn trả "ok" bất
// kể claude CLI hay token GitHub có còn hoạt động không).
package healthcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/tungnt1203/yuumi/internal/githubapp"
)

// checkTimeout giới hạn mỗi lần check 1 dependency, để 1 lệnh/request bị
// treo không chặn Check() mãi mãi (Check() chạy lúc khởi động và định kỳ
// trong Run).
const checkTimeout = 15 * time.Second

// Status là kết quả kiểm tra 1 dependency tại 1 thời điểm.
type Status struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func okStatus() Status { return Status{OK: true, Message: "ok"} }

func failStatus(err error) Status { return Status{OK: false, Message: err.Error()} }

// Report gộp kết quả check tất cả dependency tại 1 thời điểm — là giá trị
// Monitor cache lại để /health đọc, không phải gọi CLI/API thật mỗi request
// (tốn quota GitHub + làm chậm health check).
type Report struct {
	ClaudeCLI Status    `json:"claude_cli"`
	GitHubApp Status    `json:"github_app"`
	CheckedAt time.Time `json:"checked_at"`
}

// Healthy báo toàn bộ dependency có đang hoạt động không.
func (r Report) Healthy() bool {
	return r.ClaudeCLI.OK && r.GitHubApp.OK
}

// ClaudeCLICheck kiểm tra `claude` CLI có gọi được không (đã cài + đã
// authenticate). Khai báo dạng func type thay vì gọi thẳng exec.Command
// trong Monitor, để test thay bằng fake mà không cần binary `claude` cài
// sẵn trên máy chạy test — cùng cách claudecli.Reviewer đang test bằng fake
// script (xem claudecli/claude_test.go).
type ClaudeCLICheck func() error

// GitHubAppCheck xác thực GitHub App (App ID + private key) còn dùng được —
// không cần biết installation nào cả, chỉ cần xác nhận ký JWT + gọi API
// thành công (xem DefaultGitHubAppCheck).
type GitHubAppCheck func() error

// DefaultClaudeCLICheck chạy `claude auth status` thật: lệnh rẻ, không gọi
// model, in JSON có "loggedIn". Trước đây chỉ chạy `claude --version` —
// lệnh đó thành công cả khi CLI chưa đăng nhập, nên container quên truyền
// CLAUDE_CODE_OAUTH_TOKEN/ANTHROPIC_API_KEY vẫn báo healthy (issue #49).
// Không xác nhận được token còn hạn (cần gọi model thật), chỉ xác nhận đã
// cấu hình.
//
// Chưa đăng nhập thì CLI (2.1.281) vẫn in JSON loggedIn=false nhưng thoát
// exit 1 — đọc stdout trước để báo đúng nguyên nhân thay vì "không chạy
// được".
//
// Có timeout: lệnh này có thể gọi mạng (vd refresh token) và treo nếu
// container bị chặn egress — khi đó Check() lúc khởi động sẽ chặn server
// không bao giờ listen.
func DefaultClaudeCLICheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	out, runErr := exec.CommandContext(ctx, "claude", "auth", "status").Output()
	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if json.Unmarshal(out, &status) == nil && !status.LoggedIn {
		return errors.New("claude CLI chưa đăng nhập (set CLAUDE_CODE_OAUTH_TOKEN hoặc ANTHROPIC_API_KEY)")
	}
	if runErr != nil {
		// ExitError.Error() chỉ có "exit status N" — lý do thật nằm ở stderr.
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && len(exitErr.Stderr) > 0 {
			return fmt.Errorf("claude CLI không chạy được: %w: %s", runErr, bytes.TrimSpace(exitErr.Stderr))
		}
		return fmt.Errorf("claude CLI không chạy được: %w", runErr)
	}
	if !status.LoggedIn {
		return fmt.Errorf("không đọc được output của claude auth status: %q", out)
	}
	return nil
}

// DefaultGitHubAppCheck ký 1 App-level JWT (xem githubapp.GenerateAppJWT) rồi
// gọi GET /app — endpoint trả thông tin của chính App đang gọi, xác thực
// bằng App-level JWT chứ không cần installation token nào cả. Đủ để phát
// hiện App ID sai hoặc private key sai/hết hạn mà không cần biết bot đã
// được cài vào installation nào.
func DefaultGitHubAppCheck(appID string, privateKeyPEM []byte) error {
	appJWT, err := githubapp.GenerateAppJWT(appID, privateKeyPEM)
	if err != nil {
		return fmt.Errorf("không ký được App JWT (App ID hoặc private key sai): %w", err)
	}
	return checkBearerTokenAgainst("https://api.github.com/app", appJWT)
}

// checkBearerTokenAgainst tách riêng để test tự trỏ vào 1 httptest.Server
// giả lập response GitHub, thay vì phải gọi API thật.
func checkBearerTokenAgainst(url, bearerToken string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("cannot create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: checkTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("xác thực GitHub App thất bại (App ID/private key sai hoặc App đã bị xoá)")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("github api error %d", resp.StatusCode)
	}
	return nil
}

// Monitor chạy check cho các dependency của bot và cache lại kết quả lần
// check gần nhất, để handler /health đọc trạng thái mà không phải gọi
// CLI/API thật mỗi request.
type Monitor struct {
	CheckClaudeCLI ClaudeCLICheck
	CheckGitHubApp GitHubAppCheck

	mu   sync.RWMutex
	last Report
}

// NewMonitor tạo Monitor dùng check thật (DefaultClaudeCLICheck,
// DefaultGitHubAppCheck) cho App ID + private key đã cấu hình. Trước lần
// Check() đầu tiên, Last() trả Report zero-value (Healthy() == false) —
// main.go phải tự gọi Check() lúc khởi động trước khi mở route /health.
func NewMonitor(gitHubAppID string, gitHubAppPrivateKey []byte) *Monitor {
	return &Monitor{
		CheckClaudeCLI: DefaultClaudeCLICheck,
		CheckGitHubApp: func() error { return DefaultGitHubAppCheck(gitHubAppID, gitHubAppPrivateKey) },
	}
}

// Check chạy check thật cho từng dependency, cache lại kết quả rồi trả về.
// Gọi lúc khởi động để biết ngay nếu bot start trong tình trạng đã hỏng sẵn
// (claude CLI chưa authenticate, GitHub App auth sai...), thay vì phải chờ
// tới khi có webhook thật mới phát hiện ra.
func (m *Monitor) Check() Report {
	report := Report{CheckedAt: time.Now()}

	if err := m.CheckClaudeCLI(); err != nil {
		report.ClaudeCLI = failStatus(err)
	} else {
		report.ClaudeCLI = okStatus()
	}

	if err := m.CheckGitHubApp(); err != nil {
		report.GitHubApp = failStatus(err)
	} else {
		report.GitHubApp = okStatus()
	}

	m.mu.Lock()
	m.last = report
	m.mu.Unlock()

	return report
}

// Run gọi Check() lại mỗi interval cho tới khi ctx bị huỷ, để /health (và
// Docker HEALTHCHECK) phản ánh trạng thái hiện tại chứ không chỉ lúc khởi
// động: lỗi thoáng qua lúc start tự hết, còn token hỏng sau khi start thì
// bị phát hiện. onCheck (có thể nil) nhận từng Report mới, vd để log.
func (m *Monitor) Run(ctx context.Context, interval time.Duration, onCheck func(Report)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			report := m.Check()
			if onCheck != nil {
				onCheck(report)
			}
		}
	}
}

// Last trả về kết quả check gần nhất (từ lần Check() gần nhất) mà không
// chạy check mới — dùng cho handler /health, để mỗi request health check
// không tốn thêm 1 lần gọi GitHub API/exec claude CLI.
func (m *Monitor) Last() Report {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.last
}
