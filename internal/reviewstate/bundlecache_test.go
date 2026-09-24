package reviewstate

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBundleCache_SaveThenLoad(t *testing.T) {
	c := NewBundleCache(filepath.Join(t.TempDir(), "cache"))

	if _, found, err := c.LoadBundle("o/r", 1, "k1"); err != nil || found {
		t.Fatalf("LoadBundle before save: found=%v err=%v, want not found", found, err)
	}
	if err := c.SaveBundle("o/r", 1, "k1", "kết quả"); err != nil {
		t.Fatalf("SaveBundle error: %v", err)
	}
	text, found, err := c.LoadBundle("o/r", 1, "k1")
	if err != nil || !found || text != "kết quả" {
		t.Errorf("LoadBundle = %q found=%v err=%v, want \"kết quả\"", text, found, err)
	}
}

func TestBundleCache_ExpiredEntryNotFound(t *testing.T) {
	c := NewBundleCache(t.TempDir())
	if err := c.SaveBundle("o/r", 1, "k1", "cũ"); err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return time.Now().Add(bundleCacheTTL + time.Hour) }

	if _, found, _ := c.LoadBundle("o/r", 1, "k1"); found {
		t.Error("LoadBundle on expired entry: found=true, want false")
	}
}

// ClearBundles chỉ xoá entry của đúng PR đó — kể cả repo có tên là tiền tố
// của repo khác ("o/r" với "o/r_1").
func TestBundleCache_ClearOnlyThatPR(t *testing.T) {
	c := NewBundleCache(t.TempDir())
	for _, e := range []struct {
		repo string
		pr   int
	}{{"o/r", 1}, {"o/r", 12}, {"o/r_1", 2}, {"o/r", 2}} {
		if err := c.SaveBundle(e.repo, e.pr, "k", "x"); err != nil {
			t.Fatal(err)
		}
	}

	if err := c.ClearBundles("o/r", 1); err != nil {
		t.Fatalf("ClearBundles error: %v", err)
	}

	if _, found, _ := c.LoadBundle("o/r", 1, "k"); found {
		t.Error("o/r#1 still cached after ClearBundles")
	}
	for _, e := range []struct {
		repo string
		pr   int
	}{{"o/r", 12}, {"o/r_1", 2}, {"o/r", 2}} {
		if _, found, _ := c.LoadBundle(e.repo, e.pr, "k"); !found {
			t.Errorf("%s#%d was removed by ClearBundles(o/r, 1)", e.repo, e.pr)
		}
	}
}

func TestBundleCache_ClearMissingDir(t *testing.T) {
	c := NewBundleCache(filepath.Join(t.TempDir(), "nope"))
	if err := c.ClearBundles("o/r", 1); err != nil {
		t.Errorf("ClearBundles on missing dir: %v, want nil", err)
	}
}

// ClearBundles dọn luôn entry quá TTL của PR khác, giữ entry còn hạn.
func TestBundleCache_ClearRemovesExpiredOfOtherPRs(t *testing.T) {
	dir := t.TempDir()
	c := NewBundleCache(dir)
	for _, pr := range []int{1, 2, 3} {
		if err := c.SaveBundle("o/r", pr, "k", "x"); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-bundleCacheTTL - time.Hour)
	if err := os.Chtimes(c.path("o/r", 2, "k"), old, old); err != nil {
		t.Fatal(err)
	}

	if err := c.ClearBundles("o/r", 1); err != nil {
		t.Fatalf("ClearBundles error: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != filepath.Base(c.path("o/r", 3, "k")) {
		t.Errorf("remaining entries = %v, want only o/r#3", entries)
	}
}

// SaveBundle không để lại file .tmp sau khi ghi xong.
func TestBundleCache_SaveLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	if err := NewBundleCache(dir).SaveBundle("o/r", 1, "k", "x"); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".txt" {
		t.Errorf("entries = %v, want exactly 1 .txt file", entries)
	}
}
