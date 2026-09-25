package config

import (
	"encoding/base64"
	"os"
	"testing"
)

// setRequiredEnv set các biến bắt buộc để Load() không fail vì thiếu chúng —
// dùng chung cho các test chỉ muốn kiểm tra 1 biến optional cụ thể.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_WEBHOOK_SECRET", "secret")
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", base64.StdEncoding.EncodeToString([]byte("fake-pem-content")))
	t.Setenv("ALLOWED_USERS", "octocat")
}

func TestLoad_GitHubAppPrivateKey_FromBase64Env(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if string(cfg.GitHubAppPrivateKey) != "fake-pem-content" {
		t.Errorf("GitHubAppPrivateKey = %q, want %q", cfg.GitHubAppPrivateKey, "fake-pem-content")
	}
}

func TestLoad_GitHubAppPrivateKey_FromFile_TakesPrecedenceOverEnv(t *testing.T) {
	setRequiredEnv(t)

	path := t.TempDir() + "/key.pem"
	if err := os.WriteFile(path, []byte("pem-from-file"), 0o600); err != nil {
		t.Fatalf("cannot write test key file: %v", err)
	}
	t.Setenv("GITHUB_APP_PRIVATE_KEY_PATH", path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if string(cfg.GitHubAppPrivateKey) != "pem-from-file" {
		t.Errorf("GitHubAppPrivateKey = %q, want %q (file phải ưu tiên hơn base64 env)", cfg.GitHubAppPrivateKey, "pem-from-file")
	}
}

func TestLoad_GitHubAppPrivateKey_Missing(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "secret")
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("ALLOWED_USERS", "octocat")

	if _, err := Load(); err == nil {
		t.Error("Load() with no private key source: expected error, got nil")
	}
}

func TestLoad_GitHubAppPrivateKey_InvalidBase64(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "not valid base64!!")

	if _, err := Load(); err == nil {
		t.Error("Load() with invalid base64 GITHUB_APP_PRIVATE_KEY: expected error, got nil")
	}
}

func TestLoad_MaxDiffBundleChars_UnsetDefaultsToZero(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MaxDiffBundleChars != 0 {
		t.Errorf("MaxDiffBundleChars = %d, want 0 (unset -> để review package tự dùng default)", cfg.MaxDiffBundleChars)
	}
}

func TestLoad_MaxDiffBundleChars_ValidOverride(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MAX_DIFF_BUNDLE_CHARS", "20000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MaxDiffBundleChars != 20000 {
		t.Errorf("MaxDiffBundleChars = %d, want 20000", cfg.MaxDiffBundleChars)
	}
}

func TestLoad_MaxDiffBundleChars_Invalid(t *testing.T) {
	tests := []string{"abc", "0", "-100", ""}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("MAX_DIFF_BUNDLE_CHARS", raw)

			if _, err := Load(); err == nil {
				t.Errorf("Load() with MAX_DIFF_BUNDLE_CHARS=%q: expected error, got nil", raw)
			}
		})
	}
}

func TestLoad_ReviewTimeoutMinutes(t *testing.T) {
	setRequiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ReviewTimeoutMinutes != 0 {
		t.Errorf("ReviewTimeoutMinutes = %d, want 0 when unset", cfg.ReviewTimeoutMinutes)
	}

	t.Setenv("REVIEW_TIMEOUT_MINUTES", "20")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ReviewTimeoutMinutes != 20 {
		t.Errorf("ReviewTimeoutMinutes = %d, want 20", cfg.ReviewTimeoutMinutes)
	}

	for _, raw := range []string{"abc", "0", "-1"} {
		t.Setenv("REVIEW_TIMEOUT_MINUTES", raw)
		if _, err := Load(); err == nil {
			t.Errorf("Load() with REVIEW_TIMEOUT_MINUTES=%q: expected error, got nil", raw)
		}
	}
}

func TestLoad_ReviewMaxBudgetUSD(t *testing.T) {
	setRequiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ReviewMaxBudgetUSD != 0 {
		t.Errorf("ReviewMaxBudgetUSD = %v, want 0 when unset", cfg.ReviewMaxBudgetUSD)
	}

	t.Setenv("REVIEW_MAX_BUDGET_USD", "1.5")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ReviewMaxBudgetUSD != 1.5 {
		t.Errorf("ReviewMaxBudgetUSD = %v, want 1.5", cfg.ReviewMaxBudgetUSD)
	}

	for _, raw := range []string{"abc", "0", "-1", "NaN", "Inf"} {
		t.Setenv("REVIEW_MAX_BUDGET_USD", raw)
		if _, err := Load(); err == nil {
			t.Errorf("Load() with REVIEW_MAX_BUDGET_USD=%q: expected error, got nil", raw)
		}
	}
}

func TestLoad_MaxConcurrentReviews_UnsetDefaultsToZero(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MaxConcurrentReviews != 0 {
		t.Errorf("MaxConcurrentReviews = %d, want 0 (unset -> để review package tự dùng default)", cfg.MaxConcurrentReviews)
	}
}

func TestLoad_MaxConcurrentReviews_ValidOverride(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MAX_CONCURRENT_REVIEWS", "5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MaxConcurrentReviews != 5 {
		t.Errorf("MaxConcurrentReviews = %d, want 5", cfg.MaxConcurrentReviews)
	}
}

func TestLoad_MaxConcurrentReviews_Invalid(t *testing.T) {
	tests := []string{"abc", "0", "-1"}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("MAX_CONCURRENT_REVIEWS", raw)

			if _, err := Load(); err == nil {
				t.Errorf("Load() with MAX_CONCURRENT_REVIEWS=%q: expected error, got nil", raw)
			}
		})
	}
}

func TestLoad_MaxDiffBundleCharsAndMaxConcurrentReviews_Independent(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MAX_DIFF_BUNDLE_CHARS", "15000")
	t.Setenv("MAX_CONCURRENT_REVIEWS", "2")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MaxDiffBundleChars != 15000 || cfg.MaxConcurrentReviews != 2 {
		t.Errorf("got MaxDiffBundleChars=%d MaxConcurrentReviews=%d, want 15000 and 2",
			cfg.MaxDiffBundleChars, cfg.MaxConcurrentReviews)
	}
}

func TestLoad_Sandbox(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"unset keeps local", map[string]string{}, false},
		{"docker with image and work dir", map[string]string{"SANDBOX": "docker", "SANDBOX_IMAGE": "yuumi:dev", "WORK_DIR": "/srv/yuumi/work"}, false},
		// Thiếu image/work dir thì server không khởi động, thay vì mọi review
		// đều fail lúc tạo sandbox.
		{"docker without image", map[string]string{"SANDBOX": "docker", "WORK_DIR": "/srv/yuumi/work"}, true},
		{"docker without work dir", map[string]string{"SANDBOX": "docker", "SANDBOX_IMAGE": "yuumi:dev"}, true},
		// Gõ sai (vd "Docker") không được âm thầm chạy code PR trên server.
		{"unknown mode", map[string]string{"SANDBOX": "Docker"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnv(t)
			for _, k := range []string{"SANDBOX", "SANDBOX_IMAGE", "WORK_DIR"} {
				t.Setenv(k, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && cfg.Sandbox != tt.env["SANDBOX"] {
				t.Errorf("Sandbox = %q, want %q", cfg.Sandbox, tt.env["SANDBOX"])
			}
		})
	}
}
