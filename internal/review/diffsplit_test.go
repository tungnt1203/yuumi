package review

import (
	"strings"
	"testing"
)

func TestSplitDiffByFile(t *testing.T) {
	diff := "diff --git a/x.go b/x.go\n+line1\n" +
		"diff --git a/y.go b/y.go\n+line2\n-line3"

	files := splitDiffByFile(diff)

	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %q", len(files), files)
	}
	if !strings.HasPrefix(files[0], "diff --git a/x.go b/x.go") || !strings.Contains(files[0], "+line1") {
		t.Errorf("file[0] = %q", files[0])
	}
	if !strings.HasPrefix(files[1], "diff --git a/y.go b/y.go") || !strings.Contains(files[1], "+line2") {
		t.Errorf("file[1] = %q", files[1])
	}
}

func TestSplitDiffByFile_Empty(t *testing.T) {
	if got := splitDiffByFile(""); got != nil {
		t.Errorf("splitDiffByFile(\"\") = %v, want nil", got)
	}
}

func TestSplitDiffByFile_GarbageWithoutHeader(t *testing.T) {
	// Không có dòng "diff --git " nào — không panic, trả về rỗng.
	if got := splitDiffByFile("just some text\nno header here"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestTruncateDiff(t *testing.T) {
	short := "diff --git a/x b/x\n+ok"
	if got := truncateDiff(short, 1000); got != short {
		t.Errorf("truncateDiff should not touch content under budget, got %q", got)
	}

	long := strings.Repeat("a", 100)
	got := truncateDiff(long, 10)
	if len(got) <= 10 {
		t.Errorf("expected truncated output to include notice (longer than budget), got %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("a", 10)) {
		t.Errorf("expected truncated output to keep the first budgetChars bytes, got %q", got)
	}
	if !strings.Contains(got, "cắt bớt") {
		t.Errorf("expected truncation notice in output, got %q", got)
	}
}

func TestBundleDiffs_UnderBudget_ReturnsOriginalUnchanged(t *testing.T) {
	diff := "diff --git a/x.go b/x.go\n+line1"

	got := bundleDiffs(diff, 1000)

	if len(got) != 1 || got[0] != diff {
		t.Errorf("bundleDiffs() = %v, want single bundle equal to original diff", got)
	}
}

func TestBundleDiffs_EmptyDiff(t *testing.T) {
	if got := bundleDiffs("", 1000); got != nil {
		t.Errorf("bundleDiffs(\"\", ...) = %v, want nil", got)
	}
	if got := bundleDiffs("   \n", 1000); got != nil {
		t.Errorf("bundleDiffs(whitespace, ...) = %v, want nil", got)
	}
}

func TestBundleDiffs_GroupsFilesUnderBudget(t *testing.T) {
	fileA := "diff --git a/a.go b/a.go\n" + strings.Repeat("+a", 20) // ~42 chars
	fileB := "diff --git a/b.go b/b.go\n" + strings.Repeat("+b", 20)
	fileC := "diff --git a/c.go b/c.go\n" + strings.Repeat("+c", 20)
	diff := strings.Join([]string{fileA, fileB, fileC}, "\n")

	// Budget đủ cho 2 file/bundle nhưng không đủ cho cả 3.
	budget := len(fileA) + len(fileB) + 1

	bundles := bundleDiffs(diff, budget)

	if len(bundles) != 2 {
		t.Fatalf("got %d bundles, want 2: %q", len(bundles), bundles)
	}
	if !strings.Contains(bundles[0], "a.go") || !strings.Contains(bundles[0], "b.go") {
		t.Errorf("bundle[0] should contain a.go and b.go, got %q", bundles[0])
	}
	if !strings.Contains(bundles[1], "c.go") {
		t.Errorf("bundle[1] should contain c.go, got %q", bundles[1])
	}
	for i, b := range bundles {
		if len(b) > budget {
			// bundle gồm nhiều file gộp lại có thể lệch 1 chút do ký tự nối,
			// nhưng ở test này từng bundle chỉ có <=2 file vừa khít budget.
			t.Errorf("bundle[%d] length %d exceeds budget %d", i, len(b), budget)
		}
	}
}

func TestBundleDiffs_SingleHugeFileGetsOwnTruncatedBundle(t *testing.T) {
	huge := "diff --git a/generated.json b/generated.json\n" + strings.Repeat("x", 500)
	small := "diff --git a/main.go b/main.go\n+fmt.Println(1)"
	diff := huge + "\n" + small

	budget := 50
	bundles := bundleDiffs(diff, budget)

	if len(bundles) != 2 {
		t.Fatalf("got %d bundles, want 2 (huge file alone + small file alone): %q", len(bundles), bundles)
	}
	if !strings.Contains(bundles[0], "generated.json") || !strings.Contains(bundles[0], "cắt bớt") {
		t.Errorf("bundle[0] should be the truncated huge file, got %q", bundles[0])
	}
	if !strings.Contains(bundles[1], "main.go") {
		t.Errorf("bundle[1] should be the small file, got %q", bundles[1])
	}
}

func TestBundleDiffs_AllFilesFitOneBundleWhenSmall(t *testing.T) {
	fileA := "diff --git a/a.go b/a.go\n+a"
	fileB := "diff --git a/b.go b/b.go\n+b"
	diff := fileA + "\n" + fileB

	bundles := bundleDiffs(diff, 10_000)

	if len(bundles) != 1 {
		t.Fatalf("got %d bundles, want 1: %q", len(bundles), bundles)
	}
}
