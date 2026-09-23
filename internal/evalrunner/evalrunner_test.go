package evalrunner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tungnt1203/yuumi/internal/review"
)

// fakeReviewer implement review.Reviewer bằng cách ghi lại prompt/dir nhận
// được — dùng để test Run mà không phụ thuộc `claude` CLI thật (fixture
// đánh giá chất lượng model là việc chạy thủ công riêng, không phải thứ CI
// nên gọi tự động mỗi lần go test).
type fakeReviewer struct {
	gotPrompt string
	gotDir    string
	result    string
	err       error
}

func (f *fakeReviewer) Review(prompt, dir string) (string, review.CallStats, error) {
	f.gotPrompt = prompt
	f.gotDir = dir
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
	result, err := Run(reviewer, Fixture{Name: "sample", Dir: dir})
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
	if result.Response != "[]" {
		t.Errorf("Result.Response = %q, want %q", result.Response, "[]")
	}
}
