package githubapp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// withFakeGitHubAPI trỏ apiBaseURL sang 1 httptest.Server trong lúc chạy
// test, tự trả về đúng giá trị cũ khi test xong (t.Cleanup) — để test không
// ảnh hưởng lẫn nhau nếu chạy song song/tuần tự.
func withFakeGitHubAPI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	original := apiBaseURL
	apiBaseURL = server.URL
	t.Cleanup(func() { apiBaseURL = original })
}

func TestGetInstallationToken_Success(t *testing.T) {
	wantPath := "/app/installations/999/access_tokens"

	withFakeGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != wantPath {
			t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-jwt" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer test-jwt")
		}

		fmt.Fprint(w, `{"token":"ghs_abc123","expires_at":"2026-01-01T00:00:00Z"}`)
	})

	got, err := GetInstallationToken(context.Background(), "test-jwt", 999)
	if err != nil {
		t.Fatalf("GetInstallationToken() error = %v", err)
	}

	if got.Token != "ghs_abc123" {
		t.Errorf("Token = %q, want %q", got.Token, "ghs_abc123")
	}
	wantExpiresAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !got.ExpiresAt.Equal(wantExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, wantExpiresAt)
	}
}

// TestGetInstallationToken_RespectsContextCancellation xác nhận đúng vấn đề
// review PR #47 phát hiện: trước đây dùng http.NewRequest (không context) +
// http.DefaultClient (không timeout) nên nếu GitHub API treo, request block
// vô thời hạn — trong khi Provider.Token() gọi hàm này lúc giữ mutex cache,
// kéo theo mọi installation khác cũng bị chặn. Giờ phải tôn trọng ctx
// truyền vào: server cố tình treo lâu hơn ctx timeout, hàm phải trả lỗi
// ngay khi ctx hết hạn, không phải chờ tới installationTokenHTTPTimeout (10s)
// hay chờ server phản hồi.
func TestGetInstallationToken_RespectsContextCancellation(t *testing.T) {
	blockServerResponse := make(chan struct{})
	// Đăng ký SAU withFakeGitHubAPI (bên dưới): t.Cleanup chạy theo thứ tự
	// LIFO, nên cleanup này phải chạy TRƯỚC server.Close() (cleanup của
	// withFakeGitHubAPI) — ngược lại server.Close() sẽ đợi handler đang bị
	// chặn ở <-blockServerResponse thoát ra, mà kênh đó lại chưa được đóng =>
	// deadlock, treo cả test suite.
	withFakeGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		<-blockServerResponse // chỉ trả lời khi test kết thúc, giả lập GitHub treo
	})
	t.Cleanup(func() { close(blockServerResponse) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := GetInstallationToken(ctx, "test-jwt", 999)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("GetInstallationToken() with cancelled context: expected error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("GetInstallationToken() blocked %v, want trả lỗi gần ngay khi ctx hết hạn (~50ms)", elapsed)
	}
}

func TestGetInstallationToken_HTTPError(t *testing.T) {
	withFakeGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"Bad credentials"}`)
	})

	_, err := GetInstallationToken(context.Background(), "test-jwt", 999)
	if err == nil {
		t.Fatal("GetInstallationToken() with 401 response: expected error, got nil")
	}
}
