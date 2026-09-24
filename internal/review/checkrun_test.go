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
	got := reviewedCheckRunResult(reviewOutcome{
		Parsed:     true,
		Partial:    true,
		Findings:   []Finding{{Severity: "low"}},
		Header:     "header",
		CommentURL: "https://example/c",
	})
	if got.Conclusion != "success" || got.Title != "1 góp ý, còn phần chưa đếm" {
		t.Errorf("got conclusion=%q title=%q, want success / %q", got.Conclusion, got.Title, "1 góp ý, còn phần chưa đếm")
	}
}

// gateJob chạy 1 review trả đúng 1 finding HIGH, với .yuumi.yml ở base (sha
// "base1") và ở head (thư mục clone) tuỳ test. Trả về lần complete check run.
func gateJob(t *testing.T, gh *fakeGitHubClient, headConfig string) completedCheckRun {
	t.Helper()
	dir := t.TempDir()
	if headConfig != "" {
		dir = writeRepoConfig(t, headConfig)
	}
	gh.headSHA = "abc123"
	gh.diff = "diff --git a/x b/x"
	gh.checkRunID = 7
	if gh.baseSHA == "" {
		gh.baseSHA = "base1"
	}
	cleanupCalled := false
	job := &Job{
		GitHub:        gh,
		Clone:         fakeCloner(dir, nil, &cleanupCalled),
		Reviewer:      &fakeReviewer{result: `[{"category":"bug","severity":"high","message":"nil pointer"}]`},
		RepoFullName:  "octo/repo",
		IssueNumber:   5,
		PlaceholderID: 42,
	}
	job.Run()
	return onlyCompletedCheckRun(t, gh)
}

func TestSeverityGate_BlockedFindingFails(t *testing.T) {
	got := gateJob(t, &fakeGitHubClient{
		baseFiles: map[string]string{"base1:.yuumi.yml": "block_severity: [critical, high]\n"},
	}, "")

	if got.conclusion != "failure" {
		t.Errorf("conclusion = %q, want failure", got.conclusion)
	}
	if got.title != "1 góp ý ở mức chặn merge (CRITICAL/HIGH)" {
		t.Errorf("title = %q", got.title)
	}
	if !strings.Contains(got.summary, "Severity gate") {
		t.Errorf("summary should explain the gate: %s", got.summary)
	}
}

func TestSeverityGate_FindingBelowThresholdSucceeds(t *testing.T) {
	got := gateJob(t, &fakeGitHubClient{
		baseFiles: map[string]string{"base1:.yuumi.yml": "block_severity: [critical]\n"},
	}, "")

	if got.conclusion != "success" {
		t.Errorf("conclusion = %q, want success (HIGH không nằm trong [critical])", got.conclusion)
	}
}

// Không set block_severity: hành vi cũ, có finding vẫn success.
func TestSeverityGate_NotConfiguredSucceeds(t *testing.T) {
	got := gateJob(t, &fakeGitHubClient{}, "")

	if got.conclusion != "success" || got.title != "1 góp ý" {
		t.Errorf("got conclusion=%q title=%q, want success / 1 góp ý", got.conclusion, got.title)
	}
}

// Tác giả PR sửa .yuumi.yml ở head để tắt gate: không có tác dụng, gate đọc
// từ base.
func TestSeverityGate_HeadConfigCannotDisableGate(t *testing.T) {
	got := gateJob(t, &fakeGitHubClient{
		baseFiles: map[string]string{"base1:.yuumi.yml": "block_severity: [high]\n"},
	}, "block_severity: []\n")

	if got.conclusion != "failure" {
		t.Errorf("conclusion = %q, want failure (head config must not override base)", got.conclusion)
	}
}

// Ngược lại: PR tự thêm gate ở head cũng không có hiệu lực cho tới khi merge.
func TestSeverityGate_HeadConfigCannotEnableGate(t *testing.T) {
	got := gateJob(t, &fakeGitHubClient{}, "block_severity: [high]\n")

	if got.conclusion != "success" {
		t.Errorf("conclusion = %q, want success (gate chỉ đọc từ base)", got.conclusion)
	}
}

// Không đọc được config ở base: gate không áp dụng (không fail oan mọi PR vì
// API lỗi) nhưng phải nói rõ trên check run.
func TestSeverityGate_BaseConfigErrorSkipsGateWithNote(t *testing.T) {
	got := gateJob(t, &fakeGitHubClient{fileErr: errors.New("github 502")}, "")

	if got.conclusion != "success" {
		t.Errorf("conclusion = %q, want success", got.conclusion)
	}
	if !strings.Contains(got.summary, "severity gate không áp dụng") {
		t.Errorf("summary should warn gate was skipped: %s", got.summary)
	}
}

// Có finding bị chặn thì failure, kể cả khi 1 phần review lỗi.
func TestReviewedCheckRunResult_BlockedWinsOverError(t *testing.T) {
	got := reviewedCheckRunResult(reviewOutcome{
		HadError:      true,
		Parsed:        true,
		Findings:      []Finding{{Severity: "Critical "}},
		BlockSeverity: []string{"critical"},
	})
	if got.Conclusion != "failure" {
		t.Errorf("conclusion = %q, want failure", got.Conclusion)
	}
}

func TestNormalizeSeverities(t *testing.T) {
	valid, invalid := normalizeSeverities([]string{" High", "critical", "hight", "", "HIGH"})
	if strings.Join(valid, ",") != "high,critical" {
		t.Errorf("valid = %v, want [high critical]", valid)
	}
	if len(invalid) != 1 || invalid[0] != "hight" {
		t.Errorf("invalid = %v, want [hight]", invalid)
	}
}
