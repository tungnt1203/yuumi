package review

import (
	"errors"
	"strings"
	"testing"
)

type fakeGitHubClient struct {
	headSHA    string
	headSHAErr error
	diff       string
	diffErr    error
	editErr    error

	editCalled bool
	editedBody string
}

func (f *fakeGitHubClient) GetPullRequestHeadSHA(repoFullName string, pullRequestNumber int) (string, error) {
	return f.headSHA, f.headSHAErr
}

func (f *fakeGitHubClient) GetPullRequestDiff(repoFullName string, pullRequestNumber int) (string, error) {
	return f.diff, f.diffErr
}

func (f *fakeGitHubClient) EditComment(repoFullName string, commentID int64, body string) error {
	f.editCalled = true
	f.editedBody = body
	return f.editErr
}

type fakeReviewer struct {
	result string
	err    error

	called    bool
	gotPrompt string
	gotDir    string
}

func (f *fakeReviewer) Review(prompt string, dir string) (string, error) {
	f.called = true
	f.gotPrompt = prompt
	f.gotDir = dir
	return f.result, f.err
}

// fakeCloner trả về dir cố định và đánh dấu lại khi cleanup được gọi, để
// test kiểm tra cleanup luôn chạy (tránh leak thư mục tạm) mà không cần
// git/network thật.
func fakeCloner(dir string, err error, cleanupCalled *bool) Cloner {
	return func(repoFullName string, sha string) (string, func(), error) {
		if err != nil {
			return "", nil, err
		}
		return dir, func() { *cleanupCalled = true }, nil
	}
}

func TestJobRun_HappyPath(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/x b/x"}
	reviewer := &fakeReviewer{result: "trông ổn"}
	cleanupCalled := false

	job := &Job{
		GitHub:        gh,
		Clone:         fakeCloner("/tmp/fake-dir", nil, &cleanupCalled),
		Reviewer:      reviewer,
		RepoFullName:  "octo/repo",
		IssueNumber:   1,
		PlaceholderID: 42,
		UserCommand:   "review",
	}
	job.Run()

	if !reviewer.called {
		t.Fatal("expected Reviewer.Review to be called")
	}
	if reviewer.gotDir != "/tmp/fake-dir" {
		t.Errorf("Reviewer got dir %q, want %q", reviewer.gotDir, "/tmp/fake-dir")
	}
	if !strings.Contains(reviewer.gotPrompt, "diff --git a/x b/x") {
		t.Errorf("Reviewer prompt missing diff content: %s", reviewer.gotPrompt)
	}
	if !cleanupCalled {
		t.Error("expected clone cleanup to be called")
	}
	if !gh.editCalled || gh.editedBody != "trông ổn" {
		t.Errorf("expected EditComment(%q), got called=%v body=%q", "trông ổn", gh.editCalled, gh.editedBody)
	}
}

func TestJobRun_HeadSHAError_StopsEarly(t *testing.T) {
	gh := &fakeGitHubClient{headSHAErr: errors.New("boom")}
	reviewer := &fakeReviewer{}
	cloneCalled := false

	job := &Job{
		GitHub: gh,
		Clone: func(repoFullName, sha string) (string, func(), error) {
			cloneCalled = true
			return "", nil, nil
		},
		Reviewer: reviewer,
	}
	job.Run()

	if cloneCalled {
		t.Error("expected Clone not to be called when getting head SHA fails")
	}
	if reviewer.called {
		t.Error("expected Reviewer not to be called when getting head SHA fails")
	}
	if gh.editCalled {
		t.Error("expected EditComment not to be called when getting head SHA fails")
	}
}

func TestJobRun_CloneError_StopsEarly(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123"}
	reviewer := &fakeReviewer{}
	cleanupCalled := false

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("", errors.New("clone failed"), &cleanupCalled),
		Reviewer: reviewer,
	}
	job.Run()

	if reviewer.called {
		t.Error("expected Reviewer not to be called when clone fails")
	}
	if gh.editCalled {
		t.Error("expected EditComment not to be called when clone fails")
	}
}

func TestJobRun_DiffError_StillReviewsWithFallback(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", diffErr: errors.New("diff fetch failed")}
	reviewer := &fakeReviewer{result: "ok"}
	cleanupCalled := false

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, &cleanupCalled),
		Reviewer: reviewer,
	}
	job.Run()

	if !reviewer.called {
		t.Fatal("expected Reviewer.Review to still be called when diff fetch fails")
	}
	if !strings.Contains(reviewer.gotPrompt, "Không lấy được diff thật") {
		t.Errorf("expected prompt to fall back when diff fetch fails, got: %s", reviewer.gotPrompt)
	}
	if !gh.editCalled || gh.editedBody != "ok" {
		t.Errorf("expected EditComment(%q), got called=%v body=%q", "ok", gh.editCalled, gh.editedBody)
	}
}

func TestJobRun_ReviewerError_EditsFailureMessage(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123"}
	reviewer := &fakeReviewer{err: errors.New("claude timed out")}
	cleanupCalled := false

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, &cleanupCalled),
		Reviewer: reviewer,
	}
	job.Run()

	if !cleanupCalled {
		t.Error("expected clone cleanup to be called even when review fails")
	}
	if !gh.editCalled {
		t.Fatal("expected EditComment to be called with the failure message")
	}
	if !strings.Contains(gh.editedBody, "claude timed out") {
		t.Errorf("expected edited comment to mention the error, got: %s", gh.editedBody)
	}
}

func TestJobRun_ReviewerPanic_Recovered(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123"}
	cleanupCalled := false

	job := &Job{
		GitHub: gh,
		Clone:  fakeCloner("/tmp/fake-dir", nil, &cleanupCalled),
		Reviewer: reviewerFunc(func(prompt, dir string) (string, error) {
			panic("unexpected panic")
		}),
	}

	// Không được panic ra ngoài Run() — job chạy trong goroutine riêng nên
	// panic không recover sẽ crash cả process.
	job.Run()
}

// reviewerFunc cho phép dựng 1 Reviewer từ closure, dùng riêng cho test panic.
type reviewerFunc func(prompt, dir string) (string, error)

func (f reviewerFunc) Review(prompt, dir string) (string, error) {
	return f(prompt, dir)
}
