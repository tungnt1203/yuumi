package githubapi

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestEscapePath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{".yuumi.yml", ".yuumi.yml"},
		// "/" giữ nguyên để path lồng nhau vẫn đúng.
		{"config/.yuumi.yml", "config/.yuumi.yml"},
		// "?", "#", "&" không được phá cấu trúc URL (chèn query param, cắt fragment).
		{"a?ref=evil", "a%3Fref=evil"},
		{"a#b", "a%23b"},
		{"my file.yml", "my%20file.yml"},
	}
	for _, tt := range tests {
		if got := escapePath(tt.path); got != tt.want {
			t.Errorf("escapePath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// Token được lấy lại cho từng request: job dài vẫn dùng token còn hạn
// (issue #99).
func TestNewRequest_FetchesTokenPerRequest(t *testing.T) {
	calls := 0
	c := NewClientWithTokenFunc(func() (string, error) {
		calls++
		return fmt.Sprintf("tok-%d", calls), nil
	})

	for want := 1; want <= 2; want++ {
		req, err := c.newRequest("GET", "https://api.github.com/x", nil)
		if err != nil {
			t.Fatalf("newRequest() error = %v", err)
		}
		if got := req.Header.Get("Authorization"); got != fmt.Sprintf("Bearer tok-%d", want) {
			t.Errorf("request %d Authorization = %q, want Bearer tok-%d", want, got, want)
		}
	}
}

func TestNewRequest_TokenError(t *testing.T) {
	c := NewClientWithTokenFunc(func() (string, error) { return "", errors.New("github down") })

	if _, err := c.newRequest("GET", "https://api.github.com/x", nil); err == nil || !strings.Contains(err.Error(), "github down") {
		t.Errorf("newRequest() error = %v, want the token error", err)
	}
}
