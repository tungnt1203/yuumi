package review

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRepoConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, repoConfigFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", repoConfigFileName, err)
	}
	return dir
}

func TestLoadRepoConfig_NoFile_ReturnsZeroValueNoError(t *testing.T) {
	dir := t.TempDir() // không có .yuumi.yml

	cfg, err := loadRepoConfig(dir)
	if err != nil {
		t.Fatalf("loadRepoConfig() unexpected error: %v", err)
	}
	if len(cfg.Exclude) != 0 || cfg.Instructions != "" {
		t.Errorf("loadRepoConfig() = %+v, want zero-value", cfg)
	}
}

func TestLoadRepoConfig_ParsesExcludeAndInstructions(t *testing.T) {
	dir := writeRepoConfig(t, `
exclude:
  - "*.generated.go"
  - "testdata/"
instructions: |
  Review nghiêm khắc phần error handling.
  Luôn yêu cầu unit test cho hàm export.
`)

	cfg, err := loadRepoConfig(dir)
	if err != nil {
		t.Fatalf("loadRepoConfig() unexpected error: %v", err)
	}

	wantExclude := []string{"*.generated.go", "testdata/"}
	if len(cfg.Exclude) != len(wantExclude) {
		t.Fatalf("Exclude = %v, want %v", cfg.Exclude, wantExclude)
	}
	for i, p := range wantExclude {
		if cfg.Exclude[i] != p {
			t.Errorf("Exclude[%d] = %q, want %q", i, cfg.Exclude[i], p)
		}
	}

	wantInstructions := "Review nghiêm khắc phần error handling.\nLuôn yêu cầu unit test cho hàm export."
	if cfg.Instructions != wantInstructions {
		t.Errorf("Instructions = %q, want %q (đã trim khoảng trắng 2 đầu, giữ nguyên xuống dòng bên trong)", cfg.Instructions, wantInstructions)
	}
}

func TestLoadRepoConfig_InvalidYAML_ReturnsError(t *testing.T) {
	dir := writeRepoConfig(t, "exclude: [this is not valid yaml :::")

	_, err := loadRepoConfig(dir)
	if err == nil {
		t.Fatal("loadRepoConfig() expected error on invalid YAML, got nil")
	}
}

func TestLoadRepoConfig_RemovesBlankExcludeEntries(t *testing.T) {
	dir := writeRepoConfig(t, `
exclude:
  - "testdata/"
  - ""
  - "   "
`)

	cfg, err := loadRepoConfig(dir)
	if err != nil {
		t.Fatalf("loadRepoConfig() unexpected error: %v", err)
	}
	if len(cfg.Exclude) != 1 || cfg.Exclude[0] != "testdata/" {
		t.Errorf("Exclude = %v, want [\"testdata/\"] (blank entries removed)", cfg.Exclude)
	}
}

func TestLoadRepoConfig_EmptyFile_ReturnsZeroValueNoError(t *testing.T) {
	dir := writeRepoConfig(t, "")

	cfg, err := loadRepoConfig(dir)
	if err != nil {
		t.Fatalf("loadRepoConfig() unexpected error: %v", err)
	}
	if len(cfg.Exclude) != 0 || cfg.Instructions != "" {
		t.Errorf("loadRepoConfig() = %+v, want zero-value", cfg)
	}
}
