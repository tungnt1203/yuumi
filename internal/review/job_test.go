package review

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeGitHubClient struct {
	headSHA    string
	headSHAErr error
	diff       string
	diffErr    error
	editErr    error

	// changedFilesCount mặc định 0 — vô hại với các test không quan tâm đến
	// tính năng này: 0 luôn <= số file parse được (>=0), nên không bao giờ
	// tự nhiên kích hoạt cảnh báo truncation nếu test không set field này.
	changedFilesCount    int
	changedFilesCountErr error

	editCalled bool
	editedBody string
}

func (f *fakeGitHubClient) GetPullRequestHeadSHA(repoFullName string, pullRequestNumber int) (string, error) {
	return f.headSHA, f.headSHAErr
}

func (f *fakeGitHubClient) GetPullRequestDiff(repoFullName string, pullRequestNumber int) (string, error) {
	return f.diff, f.diffErr
}

func (f *fakeGitHubClient) GetPullRequestChangedFilesCount(repoFullName string, pullRequestNumber int) (int, error) {
	return f.changedFilesCount, f.changedFilesCountErr
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

	// gotPrompts ghi lại prompt của TỪNG lần gọi (theo thứ tự) — dùng cho
	// test nhiều bundle, khi 1 lần Job.Run() có thể gọi Review() nhiều lần.
	gotPrompts []string
}

func (f *fakeReviewer) Review(prompt string, dir string) (string, error) {
	f.called = true
	f.gotPrompt = prompt
	f.gotDir = dir
	f.gotPrompts = append(f.gotPrompts, prompt)
	return f.result, f.err
}

// scriptedReviewer trả về kết quả/lỗi khác nhau cho từng lần gọi Review()
// theo thứ tự — dùng để test hành vi nhiều bundle (thành công lẫn lỗi).
type scriptedReviewer struct {
	results []string
	errs    []error

	prompts []string
}

func (s *scriptedReviewer) Review(prompt string, dir string) (string, error) {
	i := len(s.prompts)
	s.prompts = append(s.prompts, prompt)

	var res string
	if i < len(s.results) {
		res = s.results[i]
	}
	var err error
	if i < len(s.errs) {
		err = s.errs[i]
	}
	return res, err
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

func TestJobRun_LargeDiff_SplitsIntoBundlesAndMergesResults(t *testing.T) {
	fileA := "diff --git a/a.go b/a.go\n+" + strings.Repeat("a", 30)
	fileB := "diff --git a/b.go b/b.go\n+" + strings.Repeat("b", 30)
	diff := fileA + "\n" + fileB

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &scriptedReviewer{results: []string{"phần A ổn", "phần B ổn"}}
	cleanupCalled := false

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, &cleanupCalled),
		Reviewer: reviewer,
		// budget chỉ đủ cho 1 file/bundle -> ép phải chia làm 2 bundle.
		BundleBudgetChars: len(fileA) + 1,
	}
	job.Run()

	if len(reviewer.prompts) != 2 {
		t.Fatalf("expected Reviewer.Review to be called 2 times (1 per bundle), got %d", len(reviewer.prompts))
	}
	if !strings.Contains(reviewer.prompts[0], "a.go") || strings.Contains(reviewer.prompts[0], "b.go") {
		t.Errorf("bundle 1 prompt should contain only a.go, got: %s", reviewer.prompts[0])
	}
	if !strings.Contains(reviewer.prompts[0], "phần 1/2") {
		t.Errorf("bundle 1 prompt missing bundle note, got: %s", reviewer.prompts[0])
	}
	if !strings.Contains(reviewer.prompts[1], "b.go") || strings.Contains(reviewer.prompts[1], "a.go") {
		t.Errorf("bundle 2 prompt should contain only b.go, got: %s", reviewer.prompts[1])
	}

	if !gh.editCalled {
		t.Fatal("expected EditComment to be called")
	}
	if !strings.Contains(gh.editedBody, "phần A ổn") || !strings.Contains(gh.editedBody, "phần B ổn") {
		t.Errorf("expected merged comment to contain both bundle results, got: %s", gh.editedBody)
	}
	if !strings.Contains(gh.editedBody, "### Phần 1/2") || !strings.Contains(gh.editedBody, "### Phần 2/2") {
		t.Errorf("expected merged comment to have section headers, got: %s", gh.editedBody)
	}
	if !cleanupCalled {
		t.Error("expected clone cleanup to be called")
	}
}

func TestJobRun_ChangedFilesMismatch_WarnsDiffMayBeTruncated(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+fmt.Println(1)" // 1 file parse được

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff, changedFilesCount: 5}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if !strings.Contains(gh.editedBody, "1/5") {
		t.Errorf("expected comment to warn about the 1/5 file mismatch, got: %s", gh.editedBody)
	}
	if !strings.Contains(gh.editedBody, "trông ổn") {
		t.Errorf("expected comment to still contain the review result, got: %s", gh.editedBody)
	}
}

func TestJobRun_ChangedFilesMatch_NoWarning(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+fmt.Println(1)"

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff, changedFilesCount: 1}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if strings.Contains(gh.editedBody, "⚠️") {
		t.Errorf("expected no truncation warning when counts match, got: %s", gh.editedBody)
	}
}

func TestJobRun_ChangedFilesCountError_ReviewsNormallyWithoutWarning(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+fmt.Println(1)"

	gh := &fakeGitHubClient{
		headSHA:              "abc123",
		diff:                 diff,
		changedFilesCountErr: errors.New("rate limited"),
	}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if !reviewer.called {
		t.Fatal("expected Reviewer.Review to still be called when changed-files lookup fails")
	}
	if strings.Contains(gh.editedBody, "⚠️") {
		t.Errorf("expected no warning when we don't know the real changed-files count, got: %s", gh.editedBody)
	}
	if !strings.Contains(gh.editedBody, "trông ổn") {
		t.Errorf("expected review result to still be posted, got: %s", gh.editedBody)
	}
}

func TestJobRun_DiffWithIgnoredFile_NotesSkippedAndStillReviewsRest(t *testing.T) {
	code := "diff --git a/main.go b/main.go\n+fmt.Println(1)"
	lock := "diff --git a/go.sum b/go.sum\n+h1:abc..."
	diff := code + "\n" + lock

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if !reviewer.called {
		t.Fatal("expected Reviewer.Review to still be called for main.go")
	}
	if strings.Contains(reviewer.gotPrompt, "go.sum") {
		t.Errorf("prompt should not contain the ignored file, got: %s", reviewer.gotPrompt)
	}
	if !strings.Contains(gh.editedBody, "Đã bỏ qua") || !strings.Contains(gh.editedBody, "go.sum") {
		t.Errorf("expected comment to note the skipped file, got: %s", gh.editedBody)
	}
	if !strings.Contains(gh.editedBody, "trông ổn") {
		t.Errorf("expected comment to still contain the review result, got: %s", gh.editedBody)
	}
}

func TestJobRun_DiffOnlyIgnoredFiles_SkipsReviewerEntirely(t *testing.T) {
	lock := "diff --git a/go.sum b/go.sum\n+h1:abc..."
	vendored := "diff --git a/vendor/x/y.go b/vendor/x/y.go\n+package y"
	diff := lock + "\n" + vendored

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{result: "không nên thấy dòng này"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if reviewer.called {
		t.Error("expected Reviewer.Review NOT to be called when every changed file is ignored")
	}
	if !gh.editCalled {
		t.Fatal("expected EditComment to be called")
	}
	if !strings.Contains(gh.editedBody, "Đã bỏ qua") {
		t.Errorf("expected comment to explain everything was skipped, got: %s", gh.editedBody)
	}
}

func TestJobRun_LargeDiff_OneBundleFails_OthersStillPostResult(t *testing.T) {
	fileA := "diff --git a/a.go b/a.go\n+" + strings.Repeat("a", 30)
	fileB := "diff --git a/b.go b/b.go\n+" + strings.Repeat("b", 30)
	diff := fileA + "\n" + fileB

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &scriptedReviewer{
		results: []string{"phần A ổn"},
		errs:    []error{nil, errors.New("claude timed out")},
	}

	job := &Job{
		GitHub:            gh,
		Clone:             fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:          reviewer,
		BundleBudgetChars: len(fileA) + 1,
	}
	job.Run()

	if !gh.editCalled {
		t.Fatal("expected EditComment to be called even when one bundle fails")
	}
	if !strings.Contains(gh.editedBody, "phần A ổn") {
		t.Errorf("expected successful bundle's result to still be posted, got: %s", gh.editedBody)
	}
	if !strings.Contains(gh.editedBody, "❌ Review thất bại: claude timed out") {
		t.Errorf("expected failed bundle's error to be reported inline, got: %s", gh.editedBody)
	}
}

// loggedCall là 1 lần gọi ReviewLogger.LogReview ghi lại được — dùng để
// test Job có gọi Logger đúng tham số hay không, không quan tâm cách log
// được lưu trữ (đó là việc của reviewlog.FileLogger).
type loggedCall struct {
	repoFullName          string
	issueNumber           int
	sha                   string
	bundleIndex, total    int
	prompt, response, err string
}

type fakeReviewLogger struct {
	calls []loggedCall
}

func (f *fakeReviewLogger) LogReview(repoFullName string, issueNumber int, sha string, bundleIndex, bundleTotal int, prompt, response, errMsg string, duration time.Duration) {
	f.calls = append(f.calls, loggedCall{
		repoFullName: repoFullName,
		issueNumber:  issueNumber,
		sha:          sha,
		bundleIndex:  bundleIndex,
		total:        bundleTotal,
		prompt:       prompt,
		response:     response,
		err:          errMsg,
	})
}

func TestJobRun_LogsEachBundleReview(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+fmt.Println(1)"

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{result: "trông ổn"}
	logger := &fakeReviewLogger{}

	job := &Job{
		GitHub:       gh,
		Clone:        fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:     reviewer,
		RepoFullName: "owner/repo",
		IssueNumber:  42,
		Logger:       logger,
	}
	job.Run()

	if len(logger.calls) != 1 {
		t.Fatalf("expected 1 log call for a single-bundle review, got %d", len(logger.calls))
	}
	call := logger.calls[0]
	if call.repoFullName != "owner/repo" || call.issueNumber != 42 || call.sha != "abc123" {
		t.Errorf("unexpected log identity, got: %+v", call)
	}
	if call.bundleIndex != 1 || call.total != 1 {
		t.Errorf("expected bundleIndex=1 total=1, got %d/%d", call.bundleIndex, call.total)
	}
	if !strings.Contains(call.prompt, "fmt.Println(1)") {
		t.Errorf("expected logged prompt to contain the diff, got: %s", call.prompt)
	}
	if call.response != "trông ổn" || call.err != "" {
		t.Errorf("expected response=%q err=%q, got response=%q err=%q", "trông ổn", "", call.response, call.err)
	}
}

func TestJobRun_LogsErrorWithEmptyResponse(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+fmt.Println(1)"

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{err: errors.New("claude timed out")}
	logger := &fakeReviewLogger{}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
		Logger:   logger,
	}
	job.Run()

	if len(logger.calls) != 1 {
		t.Fatalf("expected 1 log call, got %d", len(logger.calls))
	}
	call := logger.calls[0]
	if call.response != "" {
		t.Errorf("expected empty response logged on error, got: %q", call.response)
	}
	if call.err != "claude timed out" {
		t.Errorf("expected logged error %q, got %q", "claude timed out", call.err)
	}
}

func TestJobRun_NilLogger_DoesNotPanic(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+fmt.Println(1)"

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
		// Logger không set (nil) — hành vi mặc định trước khi có issue #9.
	}
	job.Run()

	if !gh.editCalled {
		t.Error("expected EditComment to still be called with nil Logger")
	}
}

func TestJobRun_IncludesStaticCheckReportInPrompt(t *testing.T) {
	// Module Go thật, có lỗi gofmt cố ý — để staticCheckReport (chạy thật
	// trên dir đã clone) có gì đó để báo cáo, thay vì fake dir không tồn
	// tại như các test khác (staticCheckReport tự no-op với dir đó).
	dir := writeGoModule(t, `package main

func main() {
	x:=1
	_ = x
}
`)

	gh := &fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/main.go b/main.go\n+x"}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner(dir, nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if !strings.Contains(reviewer.gotPrompt, "gofmt") {
		t.Errorf("expected prompt to include static check report, got:\n%s", reviewer.gotPrompt)
	}
}

func TestJobRun_AppliesRepoConfig(t *testing.T) {
	dir := writeRepoConfig(t, `
exclude:
  - "testdata/"
instructions: "Luôn yêu cầu unit test cho hàm export."
`)

	code := "diff --git a/main.go b/main.go\n+fmt.Println(1)"
	fixture := "diff --git a/testdata/case1.json b/testdata/case1.json\n+{}"
	diff := code + "\n" + fixture

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner(dir, nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if strings.Contains(reviewer.gotPrompt, "testdata") {
		t.Errorf("expected testdata/ to be excluded per .yuumi.yml, got prompt:\n%s", reviewer.gotPrompt)
	}
	if !strings.Contains(reviewer.gotPrompt, "Luôn yêu cầu unit test cho hàm export.") {
		t.Errorf("expected repo instructions to be included in prompt, got:\n%s", reviewer.gotPrompt)
	}
	if !strings.Contains(gh.editedBody, "trông ổn") {
		t.Errorf("expected review result in comment, got: %s", gh.editedBody)
	}
}

func TestJobRun_InvalidRepoConfig_FallsBackToDefault(t *testing.T) {
	dir := writeRepoConfig(t, "exclude: [this is not valid yaml :::")

	gh := &fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/main.go b/main.go\n+fmt.Println(1)"}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner(dir, nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	// .yuumi.yml lỗi không được chặn review — vẫn phải review bình thường
	// với default (không loại trừ thêm, không hướng dẫn riêng).
	if !gh.editCalled {
		t.Fatal("expected EditComment to still be called despite invalid .yuumi.yml")
	}
	if !strings.Contains(gh.editedBody, "trông ổn") {
		t.Errorf("expected review to still run with default config, got: %s", gh.editedBody)
	}
}
