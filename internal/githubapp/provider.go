package githubapp

import (
	"context"
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
//
// CHỈ giữ p.mu lúc đọc/ghi cache, KHÔNG giữ trong lúc gọi mạng
// (GenerateAppJWT/GetInstallationToken) — giữ mutex xuyên suốt lúc gọi mạng
// (như bản trước) khiến 1 request treo/chậm chặn theo MỌI installation khác
// đang cần Token(), kể cả những installation đã có token hợp lệ sẵn trong
// cache, mâu thuẫn với chính mục tiêu concurrency-safe của Provider (phát
// hiện qua yuumi-review tự review PR #47). Cái giá phải trả: nếu 2 goroutine
// cùng lúc xin token cho CÙNG 1 installationID đang miss cache, cả 2 có thể
// cùng gọi mạng (thay vì 1 cái chờ cái kia) — chấp nhận được vì hiếm gặp và
// chỉ tốn thêm 1 request thừa, không sai kết quả.
func (p *Provider) Token(ctx context.Context, installationID int64) (string, error) {
	if cached, ok := p.cachedToken(installationID); ok {
		return cached, nil
	}

	appJWT, err := GenerateAppJWT(p.appID, p.privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("cannot generate app jwt: %w", err)
	}

	token, err := GetInstallationToken(ctx, appJWT, installationID)
	if err != nil {
		return "", fmt.Errorf("cannot get installation token for installation %d: %w", installationID, err)
	}

	p.mu.Lock()
	p.cache[installationID] = token
	p.mu.Unlock()

	return token.Token, nil
}

// cachedToken đọc cache dưới lock, trả về (token, true) nếu còn tốt (chưa
// hết hạn/sắp hết hạn trong vòng refreshBuffer) — tách riêng để Token() giữ
// lock đúng khoảng thời gian ngắn nhất cần thiết (xem comment ở Token()).
func (p *Provider) cachedToken(installationID int64) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cached, ok := p.cache[installationID]
	if !ok || !now().Before(cached.ExpiresAt.Add(-refreshBuffer)) {
		return "", false
	}
	return cached.Token, true
}
