package evalrunner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tungnt1203/yuumi/internal/review"
	"github.com/tungnt1203/yuumi/internal/sandbox"
)

// fakeReviewer implement review.Reviewer bằng cách ghi lại prompt/dir nhận
// được — dùng để test Run mà không phụ thuộc `claude` CLI thật (fixture
// đánh giá chất lượng model là việc chạy thủ công riêng, không phải thứ CI
// nên gọi tự động mỗi lần go test).
type fakeReviewer struct {
	gotPrompt  string
	gotPrompts []string
	gotDir     string
	result     string
	err        error
}

func (f *fakeReviewer) Review(prompt string, box sandbox.Env) (string, review.CallStats, error) {
	f.gotPrompt = prompt
	f.gotPrompts = append(f.gotPrompts, prompt)
	f.gotDir = box.Dir()
	return f.result, review.CallStats{Attempts: 1, NumTurns: 1}, f.err
}

// writeFixture dựng 1 fixture tối thiểu (before/after/expected.md) dưới
// root, trả về đường dẫn thư mục fixture.
func writeFixture(t *testing.T, root, name, beforeContent, afterContent, expected string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	for sub, content := range map[string]string{
		"before/main.go": beforeContent,
		"after/main.go":  afterContent,
		"expected.md":    expected,
	} {
		full := filepath.Join(dir, sub)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadFixtures_ListsSubdirsSortedByName(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "zeta", "package main\n", "package main\n// z\n", "")
	writeFixture(t, root, "alpha", "package main\n", "package main\n// a\n", "")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("not a fixture"), 0o644); err != nil {
		t.Fatal(err)
	}

	fixtures, err := LoadFixtures(root)
	if err != nil {
		t.Fatalf("LoadFixtures() error = %v", err)
	}

	if len(fixtures) != 2 || fixtures[0].Name != "alpha" || fixtures[1].Name != "zeta" {
		t.Fatalf("LoadFixtures() = %+v, want [alpha, zeta] (file README.md should be skipped)", fixtures)
	}
}

func TestBuildFixtureDiff_ProducesUnifiedDiffOfTheInjectedChange(t *testing.T) {
	root := t.TempDir()
	dir := writeFixture(t, root, "sample",
		"package main\n\nfunc f() {}\n",
		"package main\n\nfunc f() { println(\"bug\") }\n",
		"",
	)

	diff, resultDir, cleanup, err := BuildFixtureDiff(dir)
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	if err != nil {
		t.Fatalf("BuildFixtureDiff() error = %v", err)
	}

	if !strings.Contains(diff, "diff --git") || !strings.Contains(diff, "println(\"bug\")") {
		t.Errorf("expected diff to contain the injected change, got:\n%s", diff)
	}

	got, err := os.ReadFile(filepath.Join(resultDir, "main.go"))
	if err != nil {
		t.Fatalf("expected result dir to contain the AFTER state: %v", err)
	}
	if !strings.Contains(string(got), "bug") {
		t.Errorf("result dir main.go = %q, want AFTER content", got)
	}
}

func TestRun_PassesPromptAndAfterStateDirToReviewer(t *testing.T) {
	root := t.TempDir()
	dir := writeFixture(t, root, "sample",
		"package main\n\nfunc f() {}\n",
		"package main\n\nfunc f() { println(\"bug\") }\n",
		"- [ ] Phải phát hiện println debug còn sót lại\n",
	)

	reviewer := &fakeReviewer{result: `[]`}
	result, err := Run(reviewer, Fixture{Name: "sample", Dir: dir}, 0)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(reviewer.gotPrompt, "println(\"bug\")") {
		t.Errorf("expected prompt sent to reviewer to contain the diff, got:\n%s", reviewer.gotPrompt)
	}
	if reviewer.gotDir == "" {
		t.Error("expected reviewer to be called with a non-empty dir")
	}
	if result.Expected == "" || !strings.Contains(result.Expected, "println debug") {
		t.Errorf("expected Result.Expected to contain expected.md content, got: %q", result.Expected)
	}
	if len(result.Bundles) != 1 || result.Bundles[0].Response != "[]" {
		t.Errorf("Result.Bundles = %+v, want one bundle with response %q", result.Bundles, "[]")
	}
}

// Budget nhỏ hơn diff thì fixture bị chia bundle như PR thật (issue #73).
func TestRun_SmallBudget_SplitsIntoBundles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "multi")
	for sub, content := range map[string]string{
		"before/a/a.go": "package a\n",
		"after/a/a.go":  "package a\n\nfunc A() {}\n",
		"before/b/b.go": "package b\n",
		"after/b/b.go":  "package b\n\nfunc B() {}\n",
	} {
		full := filepath.Join(dir, sub)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	reviewer := &fakeReviewer{result: `[]`}
	result, err := Run(reviewer, Fixture{Name: "multi", Dir: dir}, 100)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Bundles) != 2 || len(reviewer.gotPrompts) != 2 {
		t.Fatalf("got %d bundles / %d calls, want 2", len(result.Bundles), len(reviewer.gotPrompts))
	}
	if !strings.Contains(reviewer.gotPrompts[0], "phần 1/2") {
		t.Errorf("first prompt should carry the bundle note, got:\n%s", reviewer.gotPrompts[0])
	}
}

// File chỉ có trong after/ (PR thêm file mới) phải có trong diff.
func TestBuildFixtureDiff_IncludesNewFiles(t *testing.T) {
	dir := writeFixture(t, t.TempDir(), "newfile", "package main\n", "package main\n\nfunc f() {}\n", "")
	if err := os.MkdirAll(filepath.Join(dir, "after", "extra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "after", "extra", "new.go"), []byte("package extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diff, _, cleanup, err := BuildFixtureDiff(dir)
	if err != nil {
		t.Fatalf("BuildFixtureDiff() error = %v", err)
	}
	defer cleanup()

	for _, want := range []string{"diff --git a/main.go b/main.go", "diff --git a/extra/new.go b/extra/new.go", "+package extra"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff missing %q:\n%s", want, diff)
		}
	}
}
