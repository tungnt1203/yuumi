package config

import "testing"

// setRequiredEnv set 3 biến bắt buộc để Load() không fail vì thiếu chúng —
// dùng chung cho các test chỉ muốn kiểm tra MAX_DIFF_BUNDLE_CHARS.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_WEBHOOK_SECRET", "secret")
	t.Setenv("GITHUB_TOKEN", "token")
	t.Setenv("ALLOWED_USERS", "octocat")
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
