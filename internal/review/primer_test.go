package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPrimer_Empty_NoChangedFiles(t *testing.T) {
	if got := buildPrimer("/tmp/whatever", nil); got != "" {
		t.Errorf("buildPrimer(dir, nil) = %q, want empty", got)
	}
}

func TestBuildPrimer_ListsAllChangedFiles(t *testing.T) {
	got := buildPrimer("", []string{"a.go", "internal/review/b.go"})

	for _, want := range []string{"a.go", "internal/review/b.go", "2 file"} {
		if !strings.Contains(got, want) {
			t.Errorf("buildPrimer() missing %q, got:\n%s", want, got)
		}
	}
}

func TestBuildPrimer_IncludesFoundReadmes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# root"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "README.md"), []byte("# pkg"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := buildPrimer(dir, []string{"pkg/file.go"})

	for _, want := range []string{"README.md", "pkg/README.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("buildPrimer() missing README path %q, got:\n%s", want, got)
		}
	}
}

func TestBuildPrimer_NoReadmeFound_NoReadmeSection(t *testing.T) {
	dir := t.TempDir() // repo trống, không có README nào

	got := buildPrimer(dir, []string{"a.go"})

	if strings.Contains(got, "README") {
		t.Errorf("buildPrimer() should not mention README when none found, got:\n%s", got)
	}
}

func TestFindReadmes_RootAndFileDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# root"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal", "review", "README.md"), []byte("# review"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := findReadmes(dir, []string{"internal/review/job.go", "main.go"})

	want := []string{"README.md", "internal/review/README.md"}
	if len(got) != len(want) {
		t.Fatalf("findReadmes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("findReadmes()[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestFindReadmes_EmptyDir_NoResults(t *testing.T) {
	if got := findReadmes("", []string{"a.go"}); got != nil {
		t.Errorf("findReadmes(\"\", ...) = %v, want nil", got)
	}
	// Thư mục không tồn tại trên đĩa — không panic, chỉ trả về rỗng (vd
	// test job_test.go dùng fakeCloner với dir giả không thật sự tồn tại).
	if got := findReadmes("/no/such/dir/hopefully", []string{"a.go"}); got != nil {
		t.Errorf("findReadmes(missing dir, ...) = %v, want nil", got)
	}
}
