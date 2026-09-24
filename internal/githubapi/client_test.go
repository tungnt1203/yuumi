package githubapi

import "testing"

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
