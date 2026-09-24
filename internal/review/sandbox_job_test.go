package review

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/tungnt1203/yuumi/internal/sandbox"
)

// fakeSandbox ghi lại việc Job dùng và đóng sandbox; lệnh chạy thẳng trên
// máy như sandbox.Local.
type fakeSandbox struct {
	dir    string
	closed int
}

func (f *fakeSandbox) Dir() string { return f.dir }

func (f *fakeSandbox) Command(ctx context.Context, env []string, name string, args ...string) *exec.Cmd {
	return sandbox.Local(f.dir).Command(ctx, env, name, args...)
}

func (f *fakeSandbox) Close() error { f.closed++; return nil }

func TestJobRun_Sandbox_UsedForReviewAndClosed(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/x b/x"}
	box := &fakeSandbox{dir: "/tmp/fake-dir"}
	var gotBox sandbox.Env
	cleanupCalled := false
	job := &Job{
		GitHub: gh,
		Clone:  fakeCloner("/tmp/fake-dir", nil, &cleanupCalled),
		Reviewer: reviewerFunc(func(prompt string, b sandbox.Env) (string, CallStats, error) {
			gotBox = b
			return "ok", CallStats{Attempts: 1}, nil
		}),
		PlaceholderID: 42,
		StartSandbox: func(dir string) (sandbox.Env, error) {
			if dir != "/tmp/fake-dir" {
				t.Errorf("StartSandbox dir = %q, want clone dir", dir)
			}
			return box, nil
		},
	}
	job.Run()

	if gotBox != box {
		t.Errorf("Reviewer got sandbox %v, want the job's sandbox", gotBox)
	}
	if box.closed != 1 {
		t.Errorf("sandbox closed %d times, want 1", box.closed)
	}
	if !cleanupCalled {
		t.Error("clone cleanup not called")
	}
}

// Không tạo được sandbox (vd Docker daemon không chạy): không được lặng lẽ
// chạy code PR trên máy server — review dừng, báo thất bại.
func TestJobRun_Sandbox_StartErrorFailsReview(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/x b/x", checkRunID: 7}
	reviewer := &fakeReviewer{result: "ok"}
	cleanupCalled := false
	job := &Job{
		GitHub:        gh,
		Clone:         fakeCloner("/tmp/fake-dir", nil, &cleanupCalled),
		Reviewer:      reviewer,
		PlaceholderID: 42,
		StartSandbox: func(dir string) (sandbox.Env, error) {
			return nil, errors.New("cannot connect to the Docker daemon")
		},
	}
	job.Run()

	if reviewer.called {
		t.Error("Reviewer must not run when sandbox cannot start")
	}
	if gh.editedBody != reviewSetupFailureComment {
		t.Errorf("comment = %q, want generic failure", gh.editedBody)
	}
	if got := onlyCompletedCheckRun(t, gh); got.conclusion != "neutral" {
		t.Errorf("check run conclusion = %q, want neutral", got.conclusion)
	}
	if !cleanupCalled {
		t.Error("clone cleanup not called")
	}
}
