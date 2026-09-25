package review

import (
	"os"
	"path/filepath"
	"testing"
)

// symlinkOutside tạo trong 1 thư mục clone giả file name là symlink trỏ tới
// file nằm NGOÀI clone, trả thư mục clone.
func symlinkOutside(t *testing.T, name, content string) string {
	t.Helper()
	outside := filepath.Join(t.TempDir(), "server-secret")
	if err := os.WriteFile(outside, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
	return dir
}

// .yuumi.yml do tác giả PR kiểm soát: symlink ra ngoài clone không được đọc
// (nội dung instructions đi thẳng vào prompt).
func TestLoadRepoConfig_RejectsSymlinkOutsideClone(t *testing.T) {
	dir := symlinkOutside(t, repoConfigFileName, "instructions: SECRET_FROM_SERVER\n")

	cfg, err := loadRepoConfig(dir)
	if err == nil || cfg.Instructions != "" {
		t.Errorf("loadRepoConfig() = %+v, %v, want an error and no content", cfg, err)
	}
}

func TestLoadGitignorePatterns_RejectsSymlinkOutsideClone(t *testing.T) {
	dir := symlinkOutside(t, gitignoreFileName, "secret-pattern\n")

	patterns, err := loadGitignorePatterns(dir)
	if err == nil || len(patterns) != 0 {
		t.Errorf("loadGitignorePatterns() = %v, %v, want an error and no patterns", patterns, err)
	}
}

// Symlink tới file khác TRONG clone vẫn đọc được như trước.
func TestReadCloneFile_SymlinkInsideClone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.yml"), []byte("instructions: ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.yml", filepath.Join(dir, repoConfigFileName)); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadRepoConfig(dir)
	if err != nil || cfg.Instructions != "ok" {
		t.Errorf("loadRepoConfig() = %+v, %v, want instructions from the in-clone target", cfg, err)
	}
}
