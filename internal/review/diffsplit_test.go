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

func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"normal file", "diff --git a/internal/x.go b/internal/x.go\n+ok", "internal/x.go"},
		{"root file", "diff --git a/go.sum b/go.sum\n+ok", "go.sum"},
		{"no content after header", "diff --git a/x.go b/x.go", "x.go"},
		{"malformed", "not a diff header", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractFilePath(tt.body); got != tt.want {
				t.Errorf("extractFilePath(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestIsIgnoredPath(t *testing.T) {
	ignored := []string{
		"go.sum",
		"internal/x/go.sum",
		"vendor/github.com/foo/bar.go",
		"node_modules/react/index.js",
		"dist/bundle.js",
		"assets/logo.svg",
		"assets/logo.png",
		"app.min.js",
		"package-lock.json",
		"yarn.lock",
		"go.work.sum",
		"target/debug/build.rs",
		"backend/__pycache__/main.cpython-311.pyc",
		"src/__snapshots__/App.test.js.snap",
		"web/.next/static/chunk.js",
		"api/v1/user.pb.go",
		"api/v1/user_pb2.py",
	}
	for _, p := range ignored {
		if !isIgnoredPath(p) {
			t.Errorf("isIgnoredPath(%q) = false, want true", p)
		}
	}

	kept := []string{
		"main.go",
		"internal/review/job.go",
		"README.md",
		"cmd/server/main.go",
		"internal/targets/resolver.go", // chứa "targets/", không phải "target/"
	}
	for _, p := range kept {
		if isIgnoredPath(p) {
			t.Errorf("isIgnoredPath(%q) = true, want false", p)
		}
	}
}

func TestGroupByDirectory(t *testing.T) {
	files := []keptFile{
		{path: "pkg/a/x.go", body: "A1"},
		{path: "pkg/b/y.go", body: "B1"},
		{path: "pkg/a/x_test.go", body: "A2"},
		{path: "pkg/b/z.go", body: "B2"},
	}

	got := groupByDirectory(files)

	// Thư mục theo lần xuất hiện đầu tiên: pkg/a trước pkg/b. Trong từng
	// thư mục, giữ nguyên thứ tự tương đối gốc (A1 trước A2, B1 trước B2).
	want := []string{"A1", "A2", "B1", "B2"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("groupByDirectory() = %v, want %v", got, want)
	}
}

func TestGroupByDirectory_RootFilesShareOneGroup(t *testing.T) {
	files := []keptFile{
		{path: "a.go", body: "A"},
		{path: "b.go", body: "B"},
	}

	got := groupByDirectory(files)

	// Không có "/" trong path -> path.Dir trả về "." cho cả 2 -> cùng nhóm,
	// thứ tự giữ nguyên vì đã liền nhau sẵn.
	if len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Errorf("groupByDirectory() = %v, want [A B]", got)
	}
}

func TestBundleDiffs_KeepsSameDirectoryFilesTogether(t *testing.T) {
	// Xen kẽ: pkg/a, pkg/b, pkg/a — file pkg/a bị tách xa nhau trong diff gốc.
	fileA1 := "diff --git a/pkg/a/x.go b/pkg/a/x.go\n" + strings.Repeat("+a", 20)
	fileB := "diff --git a/pkg/b/y.go b/pkg/b/y.go\n" + strings.Repeat("+b", 20)
	fileA2 := "diff --git a/pkg/a/x_test.go b/pkg/a/x_test.go\n" + strings.Repeat("+a", 20)
	diff := strings.Join([]string{fileA1, fileB, fileA2}, "\n")

	// Budget đủ cho 2 file/bundle nhưng không đủ cho cả 3 -> ép chia 2 bundle.
	budget := len(fileA1) + len(fileA2) + 1

	bundles, _ := bundleDiffs(diff, budget)

	if len(bundles) != 2 {
		t.Fatalf("got %d bundles, want 2: %q", len(bundles), bundles)
	}
	// Cả 2 file pkg/a phải nằm CHUNG 1 bundle, dù bị xen giữa bởi pkg/b
	// trong diff gốc.
	if !strings.Contains(bundles[0], "pkg/a/x.go") || !strings.Contains(bundles[0], "pkg/a/x_test.go") {
		t.Errorf("bundle[0] should contain both pkg/a files together, got %q", bundles[0])
	}
	if !strings.Contains(bundles[1], "pkg/b/y.go") {
		t.Errorf("bundle[1] should contain pkg/b/y.go, got %q", bundles[1])
	}
}

func TestBundleDiffs_UnderBudget_ReturnsOriginalUnchanged(t *testing.T) {
	diff := "diff --git a/x.go b/x.go\n+line1"

	got, skipped := bundleDiffs(diff, 1000)

	if len(got) != 1 || got[0] != diff {
		t.Errorf("bundleDiffs() bundles = %v, want single bundle equal to original diff", got)
	}
	if skipped != nil {
		t.Errorf("skipped = %v, want nil", skipped)
	}
}

func TestBundleDiffs_EmptyDiff(t *testing.T) {
	if got, skipped := bundleDiffs("", 1000); got != nil || skipped != nil {
		t.Errorf("bundleDiffs(\"\", ...) = %v, %v, want nil, nil", got, skipped)
	}
	if got, skipped := bundleDiffs("   \n", 1000); got != nil || skipped != nil {
		t.Errorf("bundleDiffs(whitespace, ...) = %v, %v, want nil, nil", got, skipped)
	}
}

func TestBundleDiffs_GroupsFilesUnderBudget(t *testing.T) {
	fileA := "diff --git a/a.go b/a.go\n" + strings.Repeat("+a", 20) // ~42 chars
	fileB := "diff --git a/b.go b/b.go\n" + strings.Repeat("+b", 20)
	fileC := "diff --git a/c.go b/c.go\n" + strings.Repeat("+c", 20)
	diff := strings.Join([]string{fileA, fileB, fileC}, "\n")

	// Budget đủ cho 2 file/bundle nhưng không đủ cho cả 3.
	budget := len(fileA) + len(fileB) + 1

	bundles, skipped := bundleDiffs(diff, budget)

	if len(bundles) != 2 {
		t.Fatalf("got %d bundles, want 2: %q", len(bundles), bundles)
	}
	if skipped != nil {
		t.Errorf("skipped = %v, want nil", skipped)
	}
	if !strings.Contains(bundles[0], "a.go") || !strings.Contains(bundles[0], "b.go") {
		t.Errorf("bundle[0] should contain a.go and b.go, got %q", bundles[0])
	}
	if !strings.Contains(bundles[1], "c.go") {
		t.Errorf("bundle[1] should contain c.go, got %q", bundles[1])
	}
}

func TestBundleDiffs_SingleHugeFileGetsOwnTruncatedBundle(t *testing.T) {
	huge := "diff --git a/generated.json b/generated.json\n" + strings.Repeat("x", 500)
	small := "diff --git a/main.go b/main.go\n+fmt.Println(1)"
	diff := huge + "\n" + small

	budget := 50
	bundles, _ := bundleDiffs(diff, budget)

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

	bundles, _ := bundleDiffs(diff, 10_000)

	if len(bundles) != 1 {
		t.Fatalf("got %d bundles, want 1: %q", len(bundles), bundles)
	}
}

func TestBundleDiffs_FiltersIgnoredFiles_SmallPR(t *testing.T) {
	code := "diff --git a/main.go b/main.go\n+fmt.Println(1)"
	lock := "diff --git a/go.sum b/go.sum\n+h1:abc..."
	diff := code + "\n" + lock

	bundles, skipped := bundleDiffs(diff, 10_000)

	if len(bundles) != 1 {
		t.Fatalf("got %d bundles, want 1: %q", len(bundles), bundles)
	}
	if strings.Contains(bundles[0], "go.sum") {
		t.Errorf("bundle should not contain go.sum, got %q", bundles[0])
	}
	if !strings.Contains(bundles[0], "main.go") {
		t.Errorf("bundle should still contain main.go, got %q", bundles[0])
	}
	if len(skipped) != 1 || skipped[0] != "go.sum" {
		t.Errorf("skipped = %v, want [\"go.sum\"]", skipped)
	}
}

func TestBundleDiffs_AllFilesIgnored_ReturnsNoBundles(t *testing.T) {
	lock := "diff --git a/go.sum b/go.sum\n+h1:abc..."
	vendored := "diff --git a/vendor/x/y.go b/vendor/x/y.go\n+package y"
	diff := lock + "\n" + vendored

	bundles, skipped := bundleDiffs(diff, 10_000)

	if bundles != nil {
		t.Errorf("bundles = %v, want nil (everything filtered out)", bundles)
	}
	if len(skipped) != 2 {
		t.Errorf("skipped = %v, want 2 entries", skipped)
	}
}
