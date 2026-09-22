package githubapp

import (
	"fmt"
	"sync"
	"time"
)

// now cho phép test giả lập thời gian trôi (kiểm tra logic hết hạn/làm mới)
// mà không cần chờ thật — cùng kiểu seam với apiBaseURL (xem installation.go).
var now = time.Now

// refreshBuffer là khoảng thời gian trước khi token thật sự hết hạn mà
// Provider coi như "sắp hết hạn, phải xin token mới" — tránh trường hợp lấy
// token đúng lúc còn vài giây rồi request tới GitHub bị từ chối giữa chừng
// vì token hết hạn trong lúc xử lý webhook.
const refreshBuffer = 5 * time.Minute

// Provider cache installation access token theo installationID, tự gọi lại
// GitHub xin token mới khi token cũ đã hết hoặc sắp hết hạn — chỗ gọi (vd
// webhook handler) chỉ cần Token(installationID), không cần biết gì về
// JWT/cache bên trong (xem issue #47).
//
// An toàn dùng đồng thời (concurrency-safe): dispatcher xử lý nhiều webhook
// cùng lúc (xem review.Dispatcher) nên nhiều goroutine có thể gọi Token()
// cùng lúc cho cùng 1 hoặc khác installationID.
type Provider struct {
	appID         string
	privateKeyPEM []byte

	mu    sync.Mutex
	cache map[int64]InstallationToken
}

// NewProvider tạo 1 Provider mới cho 1 App (appID + private key cố định
// trong suốt vòng đời server) — nhiều installation khác nhau (nhiều repo)
// dùng chung 1 Provider vì App-level JWT (xem GenerateAppJWT) không gắn với
// installation nào cả.
func NewProvider(appID string, privateKeyPEM []byte) *Provider {
	return &Provider{
		appID:         appID,
		privateKeyPEM: privateKeyPEM,
		cache:         make(map[int64]InstallationToken),
	}
}

// Token trả về installation access token còn hạn dùng cho installationID,
// lấy từ cache nếu còn tốt, hoặc xin token mới từ GitHub nếu chưa có/sắp hết
// hạn (trong vòng refreshBuffer).
func (p *Provider) Token(installationID int64) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if cached, ok := p.cache[installationID]; ok {
		if now().Before(cached.ExpiresAt.Add(-refreshBuffer)) {
			return cached.Token, nil
		}
	}

	appJWT, err := GenerateAppJWT(p.appID, p.privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("cannot generate app jwt: %w", err)
	}

	token, err := GetInstallationToken(appJWT, installationID)
	if err != nil {
		return "", fmt.Errorf("cannot get installation token for installation %d: %w", installationID, err)
	}

	p.cache[installationID] = token
	return token.Token, nil
}
