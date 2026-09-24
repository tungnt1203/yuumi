package review

import (
	"errors"
	"strings"
	"testing"
)

// runCheckRunJob chạy 1 Job với fake GitHub đã bật check run (ID 7) và trả
// về fake để test đọc các lần gọi CreateCheckRun/CompleteCheckRun.
func runCheckRunJob(t *testing.T, gh *fakeGitHubClient, reviewer *fakeReviewer, cloneErr error) *fakeGitHubClient {
	t.Helper()
	gh.checkRunID = 7
	cleanupCalled := false
	job := &Job{
		GitHub:        gh,
		Clone:         fakeCloner("/tmp/fake-dir", cloneErr, &cleanupCalled),
		Reviewer:      reviewer,
		RepoFullName:  "octo/repo",
		IssueNumber:   5,
		PlaceholderID: 42,
		UserCommand:   "review",
	}
	job.Run()
	return gh
}

// onlyCompletedCheckRun trả về lần complete duy nhất, fail nếu số lần khác 1
// — check run không được treo (0 lần) hay bị complete 2 lần.
func onlyCompletedCheckRun(t *testing.T, gh *fakeGitHubClient) completedCheckRun {
	t.Helper()
	if len(gh.completedCheckRuns) != 1 {
		t.Fatalf("CompleteCheckRun called %d times, want 1: %+v", len(gh.completedCheckRuns), gh.completedCheckRuns)
	}
	return gh.completedCheckRuns[0]
}

func TestJobRun_CheckRun_SuccessWithFindings(t *testing.T) {
	gh := runCheckRunJob(t,
		&fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/x b/x"},
		&fakeReviewer{result: `[{"category":"bug","severity":"high","message":"nil pointer"}]`},
		nil)

	if len(gh.createCheckRunSHAs) != 1 || gh.createCheckRunSHAs[0] != "abc123" {
		t.Errorf("CreateCheckRun SHAs = %v, want [abc123]", gh.createCheckRunSHAs)
	}
	// Tên check run là thứ branch protection trỏ vào — đổi nhầm là rule
	// bắt buộc của repo không còn khớp.
	if len(gh.createCheckRunNames) != 1 || gh.createCheckRunNames[0] != "yuumi review" {
		t.Errorf("CreateCheckRun names = %v, want [yuumi review]", gh.createCheckRunNames)
	}
	got := onlyCompletedCheckRun(t, gh)
	if got.id != 7 {
		t.Errorf("completed check run id = %d, want 7", got.id)
	}
	// Chưa có severity gate (#60): có finding vẫn success.
	if got.conclusion != "success" {
		t.Errorf("conclusion = %q, want success", got.conclusion)
	}
	if got.title != "1 góp ý" {
		t.Errorf("title = %q, want %q", got.title, "1 góp ý")
	}
	wantURL := "https://github.com/octo/repo/pull/5#issuecomment-42"
	if got.detailsURL != wantURL {
		t.Errorf("detailsURL = %q, want %q", got.detailsURL, wantURL)
	}
	if !strings.Contains(got.summary, "HIGH") || !strings.Contains(got.summary, wantURL) {
		t.Errorf("summary should contain severity table and comment link: %s", got.summary)
	}
}

func TestJobRun_CheckRun_NoFindings(t *testing.T) {
	gh := runCheckRunJob(t,
		&fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/x b/x"},
		&fakeReviewer{result: `[]`},
		nil)

	got := onlyCompletedCheckRun(t, gh)
	if got.conclusion != "success" || got.title != "Không phát hiện vấn đề" {
		t.Errorf("got conclusion=%q title=%q, want success / Không phát hiện vấn đề", got.conclusion, got.title)
	}
}

// Claude lỗi ở bundle: review vẫn post (kèm lỗi) nhưng chưa đủ kết luận.
func TestJobRun_CheckRun_ReviewerErrorIsNeutral(t *testing.T) {
	gh := runCheckRunJob(t,
		&fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/x b/x"},
		&fakeReviewer{err: errors.New("claude CLI timeout")},
		nil)

	if got := onlyCompletedCheckRun(t, gh); got.conclusion != "neutral" {
		t.Errorf("conclusion = %q, want neutral", got.conclusion)
	}
}

func TestJobRun_CheckRun_CloneErrorIsNeutral(t *testing.T) {
	gh := runCheckRunJob(t,
		&fakeGitHubClient{headSHA: "abc123"},
		&fakeReviewer{},
		errors.New("fetch failed"))

	got := onlyCompletedCheckRun(t, gh)
	if got.conclusion != "neutral" || got.title != "Review thất bại" {
		t.Errorf("got conclusion=%q title=%q, want neutral / Review thất bại", got.conclusion, got.title)
	}
}

func TestJobRun_CheckRun_PostCommentErrorIsNeutral(t *testing.T) {
	gh := runCheckRunJob(t,
		&fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/x b/x", editErr: errors.New("github down")},
		&fakeReviewer{result: `[]`},
		nil)

	if got := onlyCompletedCheckRun(t, gh); got.conclusion != "neutral" {
		t.Errorf("conclusion = %q, want neutral", got.conclusion)
	}
}

// Không lấy được head SHA thì không có gì để gắn check run vào.
func TestJobRun_CheckRun_HeadSHAErrorCreatesNone(t *testing.T) {
	gh := runCheckRunJob(t,
		&fakeGitHubClient{headSHAErr: errors.New("boom")},
		&fakeReviewer{},
		nil)

	if len(gh.createCheckRunSHAs) != 0 || len(gh.completedCheckRuns) != 0 {
		t.Errorf("check run calls = create %v / complete %v, want none", gh.createCheckRunSHAs, gh.completedCheckRuns)
	}
}

// Tạo check run lỗi (vd App chưa có quyền checks) không được chặn review.
func TestJobRun_CheckRun_CreateErrorDoesNotBlockReview(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/x b/x", createCheckRunErr: errors.New("403")}
	reviewer := &fakeReviewer{result: `[]`}
	runCheckRunJob(t, gh, reviewer, nil)

	if !reviewer.called || !gh.editCalled {
		t.Errorf("review should still run and post: reviewer.called=%v editCalled=%v", reviewer.called, gh.editCalled)
	}
	if len(gh.completedCheckRuns) != 0 {
		t.Errorf("CompleteCheckRun called without a check run: %+v", gh.completedCheckRuns)
	}
}

// Panic giữa chừng vẫn phải đóng check run (neutral), không treo in_progress.
func TestJobRun_CheckRun_PanicIsNeutral(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", checkRunID: 7}
	job := &Job{
		GitHub: gh,
		Clone: func(repoFullName, sha string) (string, func(), error) {
			panic("clone exploded")
		},
		Reviewer:      &fakeReviewer{},
		RepoFullName:  "octo/repo",
		IssueNumber:   5,
		PlaceholderID: 42,
	}
	job.Run()

	if got := onlyCompletedCheckRun(t, gh); got.conclusion != "neutral" {
		t.Errorf("conclusion = %q, want neutral", got.conclusion)
	}
}

// Một bundle parse được, một bundle văn xuôi: title không được nói như thể
// đã đếm đủ góp ý.
func TestReviewedCheckRunResult_PartialTitle(t *testing.T) {
	got := reviewedCheckRunResult(false, true, true, 1, "header", "https://example/c")
	if got.Conclusion != "success" || got.Title != "1 góp ý, còn phần chưa đếm" {
		t.Errorf("got conclusion=%q title=%q, want success / %q", got.Conclusion, got.Title, "1 góp ý, còn phần chưa đếm")
	}
}
