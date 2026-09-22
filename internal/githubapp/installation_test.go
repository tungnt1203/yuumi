package githubapp

import (
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

	got, err := GetInstallationToken("test-jwt", 999)
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

func TestGetInstallationToken_HTTPError(t *testing.T) {
	withFakeGitHubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"Bad credentials"}`)
	})

	_, err := GetInstallationToken("test-jwt", 999)
	if err == nil {
		t.Fatal("GetInstallationToken() with 401 response: expected error, got nil")
	}
}
