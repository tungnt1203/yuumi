package reviewstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// defaultBundleCacheDir dùng khi BundleCache.Dir rỗng.
const defaultBundleCacheDir = "logs/bundle-cache"

// bundleCacheTTL: entry cũ hơn mức này bị coi như không có. PR bị ngắt rồi
// bỏ dở thì cache không bao giờ được xoá qua ClearBundles, TTL giữ cho kết quả quá
// cũ không bị dùng lại.
const bundleCacheTTL = 7 * 24 * time.Hour

// BundleCache lưu kết quả review của từng bundle đã chạy xong, để lần
// review lại cùng PR sau khi bị ngắt giữa chừng không phải gọi lại Claude
// cho các bundle đã xong (issue #76). Mỗi entry là 1 file
// "<repo>#<pr>#<key>.txt" trong Dir; key do review.Job tính từ prompt.
type BundleCache struct {
	Dir string

	now func() time.Time // nil thì dùng time.Now; test dùng để giả thời gian
}

func NewBundleCache(dir string) *BundleCache {
	return &BundleCache{Dir: dir}
}

func (c *BundleCache) dir() string {
	if c.Dir == "" {
		return defaultBundleCacheDir
	}
	return c.Dir
}

// prefix là phần đầu tên file của mọi entry thuộc 1 PR — ClearBundles xoá
// theo prefix này. "/" trong tên repo đổi thành "__" vì không được nằm
// trong tên file. Ngăn cách bằng "#" (tên repo GitHub không chứa "#") để
// repo "o/r" PR 1 không trùng prefix với repo "o/r_1".
func prefix(repoFullName string, issueNumber int) string {
	return fmt.Sprintf("%s#%d#", strings.ReplaceAll(repoFullName, "/", "__"), issueNumber)
}

func (c *BundleCache) path(repoFullName string, issueNumber int, key string) string {
	return filepath.Join(c.dir(), prefix(repoFullName, issueNumber)+key+".txt")
}

// LoadBundle trả kết quả đã lưu cho key. found=false khi chưa có hoặc entry
// đã quá bundleCacheTTL.
func (c *BundleCache) LoadBundle(repoFullName string, issueNumber int, key string) (text string, found bool, err error) {
	p := c.path(repoFullName, issueNumber, key)
	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	if now().Sub(info.ModTime()) > bundleCacheTTL {
		return "", false, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

// SaveBundle ghi kết quả của 1 bundle, tạo Dir nếu chưa có.
func (c *BundleCache) SaveBundle(repoFullName string, issueNumber int, key, text string) error {
	if err := os.MkdirAll(c.dir(), 0o755); err != nil {
		return fmt.Errorf("cannot create bundle cache dir: %w", err)
	}
	return os.WriteFile(c.path(repoFullName, issueNumber, key), []byte(text), 0o644)
}

// ClearBundles xoá mọi entry của 1 PR — gọi khi review PR đó đã chạy xong
// không lỗi, không cần resume nữa. Dir chưa tồn tại thì không có gì để xoá.
func (c *BundleCache) ClearBundles(repoFullName string, issueNumber int) error {
	entries, err := os.ReadDir(c.dir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	p := prefix(repoFullName, issueNumber)
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), p) {
			if err := os.Remove(filepath.Join(c.dir(), e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
