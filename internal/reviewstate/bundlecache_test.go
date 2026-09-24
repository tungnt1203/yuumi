package reviewstate

import (
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
