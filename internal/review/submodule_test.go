package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Diff gitlink đúng shape GitHub trả về cho submodule đổi commit.
const bumpDiff = `diff --git a/libs/core b/libs/core
index 1111111..2222222 160000
--- a/libs/core
+++ b/libs/core
@@ -1 +1 @@
-Subproject commit 1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
+Subproject commit 2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`

const addSubmoduleDiff = `diff --git a/vendor/ui b/vendor/ui
new file mode 160000
index 0000000..3333333
--- /dev/null
+++ b/vendor/ui
@@ -0,0 +1 @@
+Subproject commit 3333333ccccccccccccccccccccccccccccccccc`

const removeSubmoduleDiff = `diff --git a/old/dep b/old/dep
deleted file mode 160000
index 4444444..0000000
--- a/old/dep
+++ /dev/null
@@ -1 +0,0 @@
-Subproject commit 4444444ddddddddddddddddddddddddddddddddd`

const normalFileDiff = `diff --git a/main.go b/main.go
index aaa..bbb 100644
--- a/main.go
+++ b/main.go
@@ -1 +1 @@
-x
+y`

func TestSplitSubmoduleChanges(t *testing.T) {
	diff := strings.Join([]string{normalFileDiff, bumpDiff, addSubmoduleDiff, removeSubmoduleDiff}, "\n")

	rest, changes := splitSubmoduleChanges(diff)

	if rest != normalFileDiff {
		t.Errorf("rest = %q, want only the normal file diff", rest)
	}
	want := []submoduleChange{
		{Path: "libs/core", OldSHA: "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", NewSHA: "2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{Path: "vendor/ui", NewSHA: "3333333ccccccccccccccccccccccccccccccccc"},
		{Path: "old/dep", OldSHA: "4444444ddddddddddddddddddddddddddddddddd"},
	}
	if len(changes) != len(want) {
		t.Fatalf("changes = %+v, want %+v", changes, want)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Errorf("changes[%d] = %+v, want %+v", i, changes[i], want[i])
		}
	}
}

// File thường có "160000" trong nội dung không bị coi là submodule.
func TestSplitSubmoduleChanges_IgnoresNormalFiles(t *testing.T) {
	diff := "diff --git a/n.txt b/n.txt\nindex aaa..bbb 100644\n--- a/n.txt\n+++ b/n.txt\n@@ -1 +1 @@\n-limit 160000\n+Subproject commit 160000"

	rest, changes := splitSubmoduleChanges(diff)
	if len(changes) != 0 || rest != diff {
		t.Errorf("got changes %+v, want the diff untouched", changes)
	}
}

// File thường bị thay bằng submodule: git không in "old mode 100644 / new
// mode 160000" mà tách thành 2 mục — xoá file cũ và "new file mode 160000"
// (output thật của git diff, kể cả với -M). Mục xoá file vẫn đi vào prompt,
// mục gitlink được nhận diện.
func TestSplitSubmoduleChanges_FileReplacedBySubmodule(t *testing.T) {
	deleted := "diff --git a/dep b/dep\ndeleted file mode 100644\nindex 45b983b..0000000\n--- a/dep\n+++ /dev/null\n@@ -1 +0,0 @@\n-hi"
	added := "diff --git a/dep b/dep\nnew file mode 160000\nindex 0000000..1111111\n--- /dev/null\n+++ b/dep\n@@ -0,0 +1 @@\n+Subproject commit 1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	rest, changes := splitSubmoduleChanges(deleted + "\n" + added)

	if rest != deleted {
		t.Errorf("rest = %q, want only the deleted regular file", rest)
	}
	want := submoduleChange{Path: "dep", NewSHA: "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if len(changes) != 1 || changes[0] != want {
		t.Errorf("changes = %+v, want [%+v]", changes, want)
	}
}

func TestSubmoduleNote(t *testing.T) {
	_, changes := splitSubmoduleChanges(strings.Join([]string{bumpDiff, addSubmoduleDiff, removeSubmoduleDiff}, "\n"))

	note := submoduleNote(changes)
	for _, want := range []string{"chưa được review", "`libs/core`: `1111111` → `2222222`", "`vendor/ui`: thêm mới tại `3333333`"} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q:\n%s", want, note)
		}
	}
	// Gỡ submodule không đưa code mới vào: không cần review.
	if strings.Contains(note, "old/dep") {
		t.Errorf("removed submodule should not be listed:\n%s", note)
	}
	if got := unreviewedSubmoduleCount(changes); got != 2 {
		t.Errorf("unreviewedSubmoduleCount = %d, want 2", got)
	}
}

func TestGitmodulesURLFindings(t *testing.T) {
	base := parseGitmodules([]byte(`[submodule "core"]
	path = libs/core
	url = https://github.com/octo/core.git
[submodule "gone"]
	path = old/dep
	url = https://github.com/octo/dep.git
`))
	head := parseGitmodules([]byte(`[submodule "core"]
	path = libs/core
	url = https://github.com/evil/core.git
[submodule "ui"]
	path = vendor/ui
	url = https://github.com/octo/ui.git
`))

	findings := gitmodulesURLFindings(base, head)
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want exactly the changed URL (added/removed do not count)", findings)
	}
	f := findings[0]
	if f.Category != "security" || f.Severity != "high" || f.File != ".gitmodules" {
		t.Errorf("finding = %+v, want security/high on .gitmodules", f)
	}
	if !strings.Contains(f.Message, "github.com/octo/core.git") || !strings.Contains(f.Message, "github.com/evil/core.git") {
		t.Errorf("message should name both URLs: %s", f.Message)
	}
}

// .gitmodules là file tác giả PR kiểm soát: symlink trỏ ra ngoài thư mục
// clone không được đọc.
func TestReadHeadGitmodules_RejectsSymlinkOutsideClone(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("[s]\n\tpath = x\n\turl = SECRET\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, ".gitmodules")); err != nil {
		t.Fatal(err)
	}

	urls, err := readHeadGitmodules(dir)
	if err == nil {
		t.Errorf("readHeadGitmodules() = %v, want an error for a symlink escaping the clone", urls)
	}
}

func TestReadHeadGitmodules_Missing(t *testing.T) {
	urls, err := readHeadGitmodules(t.TempDir())
	if err != nil || len(urls) != 0 {
		t.Errorf("readHeadGitmodules() = %v, %v, want empty and no error", urls, err)
	}
}

// PR chỉ bump submodule: không gọi Claude, không báo sạch, check run neutral.
func TestJobRun_SubmoduleOnly_NotReportedClean(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", diff: bumpDiff, checkRunID: 7}
	reviewer := &fakeReviewer{result: `{"findings":[]}`}
	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner(t.TempDir(), nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if reviewer.called {
		t.Error("reviewer should not be called when the PR only bumps a submodule")
	}
	if strings.Contains(gh.editedBody, "Không phát hiện vấn đề") {
		t.Errorf("a submodule-only PR must not be reported clean:\n%s", gh.editedBody)
	}
	for _, want := range []string{"Review chưa đủ để kết luận", "`libs/core`: `1111111` → `2222222`"} {
		if !strings.Contains(gh.editedBody, want) {
			t.Errorf("comment missing %q:\n%s", want, gh.editedBody)
		}
	}
	got := onlyCompletedCheckRun(t, gh)
	if got.conclusion != "neutral" || got.title != "1 submodule chưa được review" {
		t.Errorf("check run = %s / %q, want neutral / 1 submodule chưa được review", got.conclusion, got.title)
	}
}

// PR vừa sửa file vừa bump submodule: gitlink không vào prompt, và "0 góp
// ý" của phần file thường không được thành "✅ không có vấn đề".
func TestJobRun_MixedDiff_ExcludesGitlinkFromPrompt(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", diff: normalFileDiff + "\n" + bumpDiff, checkRunID: 7}
	reviewer := &fakeReviewer{result: `{"findings":[]}`}
	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner(t.TempDir(), nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if !strings.Contains(reviewer.gotPrompt, "main.go") {
		t.Errorf("prompt should still contain the normal file diff:\n%s", reviewer.gotPrompt)
	}
	if strings.Contains(reviewer.gotPrompt, "Subproject commit") {
		t.Errorf("prompt should not contain the gitlink diff:\n%s", reviewer.gotPrompt)
	}
	if strings.Contains(gh.editedBody, "Không phát hiện vấn đề") {
		t.Errorf("must not claim clean while a submodule is unreviewed:\n%s", gh.editedBody)
	}
	if got := onlyCompletedCheckRun(t, gh); got.conclusion != "neutral" {
		t.Errorf("check run conclusion = %s, want neutral", got.conclusion)
	}
}

// PR đổi URL của submodule đã có: finding security được đếm và hiện trong
// comment.
func TestJobRun_GitmodulesURLChanged_ReportsSecurityFinding(t *testing.T) {
	gitmodulesDiff := "diff --git a/.gitmodules b/.gitmodules\nindex aaa..bbb 100644\n--- a/.gitmodules\n+++ b/.gitmodules\n@@ -2,2 +2,2 @@\n \tpath = libs/core\n-\turl = https://github.com/octo/core.git\n+\turl = https://github.com/evil/core.git"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte("[submodule \"core\"]\n\tpath = libs/core\n\turl = https://github.com/evil/core.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gh := &fakeGitHubClient{
		headSHA: "abc123", baseSHA: "base1", checkRunID: 7,
		diff:      gitmodulesDiff,
		baseFiles: map[string]string{"base1:.gitmodules": "[submodule \"core\"]\n\tpath = libs/core\n\turl = https://github.com/octo/core.git\n"},
	}
	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner(dir, nil, new(bool)),
		Reviewer: &fakeReviewer{result: `{"findings":[]}`},
	}
	job.Run()

	for _, want := range []string{"### Submodule", "github.com/evil/core.git", "Tổng: 1 góp ý"} {
		if !strings.Contains(gh.editedBody, want) {
			t.Errorf("comment missing %q:\n%s", want, gh.editedBody)
		}
	}
	if got := onlyCompletedCheckRun(t, gh); got.title != "1 góp ý" {
		t.Errorf("check run title = %q, want 1 góp ý", got.title)
	}
}
