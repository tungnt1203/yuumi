package githubapp

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// withFakeInstallationTokenAPI giả lập endpoint access_tokens, đếm số lần bị
// gọi (requestCount) và trả token có expires_at = now()+ttl mỗi lần — dùng
// để kiểm tra Provider có cache đúng (không gọi thừa) hay không.
func withFakeInstallationTokenAPI(t *testing.T, ttl time.Duration) *int32 {
	t.Helper()
	var requestCount int32

	withFakeGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		expiresAt := now().Add(ttl).UTC().Format(time.RFC3339)
		fmt.Fprintf(w, `{"token":"ghs_token_%d","expires_at":%q}`, requestCount, expiresAt)
	})

	return &requestCount
}

func TestProvider_Token_CachesUntilNearExpiry(t *testing.T) {
	requestCount := withFakeInstallationTokenAPI(t, time.Hour)

	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now = func() time.Time { return fixedNow }
	t.Cleanup(func() { now = time.Now })

	p := NewProvider("app-id", generateTestPrivateKeyPEM(t))

	token1, err := p.Token(999)
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	token2, err := p.Token(999)
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}

	if token1 != token2 {
		t.Errorf("expected cached token on 2nd call, got token1=%q token2=%q", token1, token2)
	}
	if got := atomic.LoadInt32(requestCount); got != 1 {
		t.Errorf("requestCount = %d, want 1 (2nd call should hit cache)", got)
	}
}

func TestProvider_Token_RefreshesWhenNearExpiry(t *testing.T) {
	requestCount := withFakeInstallationTokenAPI(t, 2*time.Minute) // < refreshBuffer

	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now = func() time.Time { return fixedNow }
	t.Cleanup(func() { now = time.Now })

	p := NewProvider("app-id", generateTestPrivateKeyPEM(t))

	token1, err := p.Token(999)
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	token2, err := p.Token(999)
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}

	if token1 == token2 {
		t.Errorf("expected fresh token on 2nd call (token1 expires within refreshBuffer), got same token %q both times", token1)
	}
	if got := atomic.LoadInt32(requestCount); got != 2 {
		t.Errorf("requestCount = %d, want 2 (2nd call should refresh)", got)
	}
}

func TestProvider_Token_SeparateCachePerInstallation(t *testing.T) {
	requestCount := withFakeInstallationTokenAPI(t, time.Hour)

	p := NewProvider("app-id", generateTestPrivateKeyPEM(t))

	if _, err := p.Token(111); err != nil {
		t.Fatalf("Token(111) error = %v", err)
	}
	if _, err := p.Token(222); err != nil {
		t.Fatalf("Token(222) error = %v", err)
	}

	if got := atomic.LoadInt32(requestCount); got != 2 {
		t.Errorf("requestCount = %d, want 2 (2 installations khác nhau, mỗi cái xin token riêng)", got)
	}
}
