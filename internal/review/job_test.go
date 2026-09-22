package review

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	// compareDiff/compareDiffErr là kết quả GetCompareDiff trả về — dùng
	// cho test issue #21 (review lần 2 trở đi chỉ lấy phần thay đổi mới).
	compareDiff    string
	compareDiffErr error

	// gotCompareBase/gotCompareHead ghi lại tham số GetCompareDiff được gọi
	// với gì, để test assert đúng SHA cũ/mới được dùng.
	compareCalled  bool
	gotCompareBase string
	gotCompareHead string

	// changedFilesCount mặc định 0 — vô hại với các test không quan tâm đến
	// tính năng này: 0 luôn <= số file parse được (>=0), nên không bao giờ
	// tự nhiên kích hoạt cảnh báo truncation nếu test không set field này.
	changedFilesCount    int
	changedFilesCountErr error

	editCalled bool
	editedBody string

	// createReviewErr/createReviewCalls mô phỏng CreateReview (issue #5) —
	// createReviewCalls ghi lại TỪNG lần gọi (dù Job hiện chỉ gọi tối đa 1
	// lần/lần review) để test có thể assert số lần gọi lẫn tham số.
	createReviewErr   error
	createReviewCalls []createReviewCall
}

// createReviewCall ghi lại tham số 1 lần gọi CreateReview — dùng để test
// assert Job gửi đúng commitSHA/comments mà không cần biết
// githubapi.Client.CreateReview build request HTTP thật ra sao.
type createReviewCall struct {
	commitSHA    string
	body         string
	commentsJSON string
}

func (f *fakeGitHubClient) GetPullRequestHeadSHA(repoFullName string, pullRequestNumber int) (string, error) {
	return f.headSHA, f.headSHAErr
}

func (f *fakeGitHubClient) GetPullRequestDiff(repoFullName string, pullRequestNumber int) (string, error) {
	return f.diff, f.diffErr
}

func (f *fakeGitHubClient) GetCompareDiff(repoFullName string, baseSHA string, headSHA string) (string, error) {
	f.compareCalled = true
	f.gotCompareBase = baseSHA
	f.gotCompareHead = headSHA
	return f.compareDiff, f.compareDiffErr
}

func (f *fakeGitHubClient) GetPullRequestChangedFilesCount(repoFullName string, pullRequestNumber int) (int, error) {
	return f.changedFilesCount, f.changedFilesCountErr
}

func (f *fakeGitHubClient) EditComment(repoFullName string, commentID int64, body string) error {
	f.editCalled = true
	f.editedBody = body
	return f.editErr
}

func (f *fakeGitHubClient) CreateReview(repoFullName string, pullRequestNumber int, commitSHA string, body string, commentsJSON []byte) error {
	f.createReviewCalls = append(f.createReviewCalls, createReviewCall{
		commitSHA:    commitSHA,
		body:         body,
		commentsJSON: string(commentsJSON),
	})
	return f.createReviewErr
}

// fakeStateStore implement ReviewStateStore bằng 1 map trong bộ nhớ — dùng
// cho test issue #21 thay vì phải ghi file thật (xem reviewstate.FileStore
// cho implementation thật).
type fakeStateStore struct {
	shas map[string]string

	lastSHAErr error
	setSHAErr  error

	// gotSetSHA/setCalled ghi lại lần SetLastReviewedSHA gần nhất được gọi,
	// để test assert Run() có/không lưu state sau khi review xong.
	setCalled bool
	gotSetSHA string
}

func newFakeStateStore(seed map[string]string) *fakeStateStore {
	if seed == nil {
		seed = map[string]string{}
	}
	return &fakeStateStore{shas: seed}
}

func stateKey(repoFullName string, issueNumber int) string {
	return fmt.Sprintf("%s#%d", repoFullName, issueNumber)
}

func (f *fakeStateStore) LastReviewedSHA(repoFullName string, issueNumber int) (string, bool, error) {
	if f.lastSHAErr != nil {
		return "", false, f.lastSHAErr
	}
	sha, found := f.shas[stateKey(repoFullName, issueNumber)]
	return sha, found, nil
}

func (f *fakeStateStore) SetLastReviewedSHA(repoFullName string, issueNumber int, sha string) error {
	f.setCalled = true
	f.gotSetSHA = sha
	if f.setSHAErr != nil {
		return f.setSHAErr
	}
	f.shas[stateKey(repoFullName, issueNumber)] = sha
	return nil
}

type fakeReviewer struct {
	result   string
	err      error
	attempts int // 0 nghĩa là "chưa chỉ định", Review() trả về 1 (không retry)
	numTurns int // 0 nghĩa là "chưa chỉ định" (fake không giả lập num_turns thật)

	called    bool
	gotPrompt string
	gotDir    string

	// gotPrompts ghi lại prompt của TỪNG lần gọi (theo thứ tự) — dùng cho
	// test nhiều bundle, khi 1 lần Job.Run() có thể gọi Review() nhiều lần.
	gotPrompts []string
}

func (f *fakeReviewer) Review(prompt string, dir string) (string, int, int, error) {
	f.called = true
	f.gotPrompt = prompt
	f.gotDir = dir
	f.gotPrompts = append(f.gotPrompts, prompt)
	attempts := f.attempts
	if attempts == 0 {
		attempts = 1
	}
	return f.result, attempts, f.numTurns, f.err
}

// scriptedReviewer trả về kết quả/lỗi khác nhau cho từng lần gọi Review()
// theo thứ tự — dùng để test hành vi nhiều bundle (thành công lẫn lỗi).
type scriptedReviewer struct {
	results []string
	errs    []error

	prompts []string
}

func (s *scriptedReviewer) Review(prompt string, dir string) (string, int, int, error) {
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
	return res, 1, 0, err
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
		GitHub:        gh,
		PlaceholderID: 42,
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
	if !gh.editCalled || !strings.Contains(gh.editedBody, "boom") {
		t.Errorf("expected EditComment to report the head SHA error, got called=%v body=%q", gh.editCalled, gh.editedBody)
	}
}

func TestJobRun_CloneError_StopsEarly(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123"}
	reviewer := &fakeReviewer{}
	cleanupCalled := false

	job := &Job{
		GitHub:        gh,
		PlaceholderID: 42,
		Clone:         fakeCloner("", errors.New("clone failed"), &cleanupCalled),
		Reviewer:      reviewer,
	}
	job.Run()

	if reviewer.called {
		t.Error("expected Reviewer not to be called when clone fails")
	}
	if !gh.editCalled || !strings.Contains(gh.editedBody, "clone failed") {
		t.Errorf("expected EditComment to report the clone error, got called=%v body=%q", gh.editCalled, gh.editedBody)
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
		GitHub:        gh,
		PlaceholderID: 42,
		Clone:         fakeCloner("/tmp/fake-dir", nil, &cleanupCalled),
		Reviewer: reviewerFunc(func(prompt, dir string) (string, int, int, error) {
			panic("unexpected panic")
		}),
	}

	// Không được panic ra ngoài Run() — job chạy trong goroutine riêng nên
	// panic không recover sẽ crash cả process.
	job.Run()

	if !cleanupCalled {
		t.Error("expected clone cleanup to be called when review panics")
	}
	if !gh.editCalled || !strings.Contains(gh.editedBody, "unexpected panic") {
		t.Errorf("expected EditComment to report the panic, got called=%v body=%q", gh.editCalled, gh.editedBody)
	}
}

func TestJobRun_PanicAfterCommentPosted_DoesNotOverwrite(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/x b/x"}
	reviewer := &fakeReviewer{result: "trông ổn"}
	cleanupCalled := false

	job := &Job{
		GitHub:        gh,
		PlaceholderID: 42,
		Clone:         fakeCloner("/tmp/fake-dir", nil, &cleanupCalled),
		Reviewer:      reviewer,
		RepoFullName:  "owner/repo",
		IssueNumber:   7,
		StateStore:    panickingStateStore{},
	}
	job.Run()

	if gh.editedBody != "trông ổn" {
		t.Errorf("posted review was overwritten after a later panic, body=%q", gh.editedBody)
	}
}

// panickingStateStore nổ lúc lưu SHA, sau khi comment kết quả đã được post.
type panickingStateStore struct{}

func (panickingStateStore) LastReviewedSHA(string, int) (string, bool, error) {
	return "", false, nil
}

func (panickingStateStore) SetLastReviewedSHA(string, int, string) error {
	panic("state store exploded")
}

// reviewerFunc cho phép dựng 1 Reviewer từ closure, dùng riêng cho test panic.
type reviewerFunc func(prompt, dir string) (string, int, int, error)

func (f reviewerFunc) Review(prompt, dir string) (string, int, int, error) {
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
	// Phần diff thật (```diff ... ```) của mỗi bundle chỉ chứa file của
	// riêng nó — primer (issue #18) liệt kê CẢ HAI file ở đầu prompt là chủ
	// đích (ngữ cảnh dùng chung), nên không còn assert "chỉ chứa a.go"/"chỉ
	// chứa b.go" trên toàn bộ prompt nữa, chỉ trên phần diff.
	diffSection := func(prompt string) string {
		_, rest, _ := strings.Cut(prompt, "```diff\n")
		body, _, _ := strings.Cut(rest, "\n```")
		return body
	}
	if body := diffSection(reviewer.prompts[0]); !strings.Contains(body, "a.go") || strings.Contains(body, "b.go") {
		t.Errorf("bundle 1 diff section should contain only a.go, got: %s", body)
	}
	if !strings.Contains(reviewer.prompts[0], "phần 1/2") {
		t.Errorf("bundle 1 prompt missing bundle note, got: %s", reviewer.prompts[0])
	}
	if body := diffSection(reviewer.prompts[1]); !strings.Contains(body, "b.go") || strings.Contains(body, "a.go") {
		t.Errorf("bundle 2 diff section should contain only b.go, got: %s", body)
	}
	// Primer chia sẻ được nhúng y hệt vào MỌI bundle (issue #18).
	for i, p := range reviewer.prompts {
		if !strings.Contains(p, "a.go") || !strings.Contains(p, "b.go") {
			t.Errorf("bundle %d prompt should contain primer listing both a.go and b.go, got: %s", i+1, p)
		}
	}
	if reviewer.prompts[0][:strings.Index(reviewer.prompts[0], "```diff")] != reviewer.prompts[1][:strings.Index(reviewer.prompts[1], "```diff")] {
		t.Errorf("expected identical primer (everything before the diff fence) across bundles of the same PR, got:\nbundle1: %s\nbundle2: %s", reviewer.prompts[0], reviewer.prompts[1])
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

// TestJobRun_SingleBundle_NoPrimer đảm bảo PR bình thường (không bị chia
// bundle) không tốn công build/nhúng primer (issue #18) — primer chỉ có giá
// trị khi có NHIỀU bundle cần chia sẻ ngữ cảnh với nhau.
func TestJobRun_SingleBundle_NoPrimer(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/main.go b/main.go\n+x"}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if strings.Contains(reviewer.gotPrompt, "Ngữ cảnh dùng chung") {
		t.Errorf("expected no primer for a single-bundle PR, got prompt: %s", reviewer.gotPrompt)
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
	attempts              int
	numTurns              int
}

type fakeReviewLogger struct {
	calls []loggedCall
}

func (f *fakeReviewLogger) LogReview(repoFullName string, issueNumber int, sha string, bundleIndex, bundleTotal int, prompt, response, errMsg string, duration time.Duration, attempts int, numTurns int) {
	f.calls = append(f.calls, loggedCall{
		repoFullName: repoFullName,
		issueNumber:  issueNumber,
		sha:          sha,
		bundleIndex:  bundleIndex,
		total:        bundleTotal,
		prompt:       prompt,
		response:     response,
		err:          errMsg,
		attempts:     attempts,
		numTurns:     numTurns,
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
	if call.attempts != 1 {
		t.Errorf("expected attempts=1 for a review that succeeded on the first try, got %d", call.attempts)
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

// TestJobRun_LogsAttemptsFromReviewer đảm bảo Job chuyển đúng số lần thử
// (attempts) mà Reviewer.Review báo về cho Logger — không tự bịa ra 1
// (xem claudecli.Reviewer retry, issue #28).
func TestJobRun_LogsAttemptsFromReviewer(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+fmt.Println(1)"

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{result: "trông ổn", attempts: 3}
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
	if got := logger.calls[0].attempts; got != 3 {
		t.Errorf("expected logged attempts=3 (as reported by Reviewer), got %d", got)
	}
}

// TestJobRun_LogsNumTurnsFromReviewer đảm bảo Job chuyển đúng num_turns mà
// Reviewer.Review báo về cho Logger — tín hiệu để dò xem model có thực sự
// đọc thêm file ngoài diff hay không (xem claudecli.ClaudeResult, issue
// #20).
func TestJobRun_LogsNumTurnsFromReviewer(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+fmt.Println(1)"

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{result: "trông ổn", numTurns: 7}
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
	if got := logger.calls[0].numTurns; got != 7 {
		t.Errorf("expected logged numTurns=7 (as reported by Reviewer), got %d", got)
	}
}

// TestJobRun_StructuredFindings_RenderedInComment_RawJSONLogged xác nhận
// khi Reviewer trả đúng JSON array theo format yêu cầu (issue #26), comment
// post lên GitHub là bản render có phân loại severity (không phải JSON
// thô), nhưng reviewlog vẫn ghi lại đúng JSON gốc — để debug parseFindings
// khi cần mà không phụ thuộc vào cách hiển thị.
func TestJobRun_StructuredFindings_RenderedInComment_RawJSONLogged(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+fmt.Println(1)"
	rawJSON := `[{"category":"bug","severity":"critical","message":"nil pointer dereference"}]`

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{result: rawJSON}
	logger := &fakeReviewLogger{}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
		Logger:   logger,
	}
	job.Run()

	if !gh.editCalled {
		t.Fatal("expected EditComment to be called")
	}
	if strings.Contains(gh.editedBody, rawJSON) {
		t.Errorf("expected comment to be rendered, not raw JSON, got: %s", gh.editedBody)
	}
	if !strings.Contains(gh.editedBody, "🔴") || !strings.Contains(gh.editedBody, "nil pointer dereference") {
		t.Errorf("expected comment to contain the rendered finding, got: %s", gh.editedBody)
	}

	if len(logger.calls) != 1 {
		t.Fatalf("expected 1 log call, got %d", len(logger.calls))
	}
	if logger.calls[0].response != rawJSON {
		t.Errorf("expected logged response to be the raw JSON (not the rendered comment), got: %s", logger.calls[0].response)
	}
}

// TestJobRun_StructuredFindings_IncludesReviewHeader đảm bảo comment tổng
// hợp có banner tổng quan (renderReviewHeader) ngay khi có ít nhất 1 bundle
// parse được JSON — khác TestJobRun_UnparsableResult_FallsBackToRawText bên
// dưới, nơi Claude trả văn xuôi tự do và KHÔNG có banner này (xem cờ
// anyParsed, Job.reviewBundles).
func TestJobRun_StructuredFindings_IncludesReviewHeader(t *testing.T) {
	rawJSON := `[{"category":"bug","severity":"critical","message":"nil pointer dereference"}]`

	gh := &fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/main.go b/main.go\n+fmt.Println(1)"}
	reviewer := &fakeReviewer{result: rawJSON}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if !strings.Contains(gh.editedBody, "## 🟣 Yuumi Review") {
		t.Errorf("expected comment to include the review header, got: %s", gh.editedBody)
	}
	if !strings.Contains(gh.editedBody, "Tổng: 1 góp ý") {
		t.Errorf("expected review header to count the 1 parsed finding, got: %s", gh.editedBody)
	}
}

// TestJobRun_MixedBundles_HeaderWarnsCountIsPartial đảm bảo khi PR bị chia
// nhiều bundle và CHỈ MỘT PHẦN parse được JSON (phần còn lại fallback raw
// text), header vẫn hiện (còn dữ liệu có cấu trúc để tổng hợp) nhưng phải
// cảnh báo rõ "Tổng: N" không đại diện cho toàn bộ PR — nếu không cảnh báo,
// người đọc dễ tưởng lầm N là đầy đủ trong khi phần raw text bên dưới có
// thể còn thêm vấn đề chưa được đếm (PR review issue #57).
func TestJobRun_MixedBundles_HeaderWarnsCountIsPartial(t *testing.T) {
	fileA := "diff --git a/a.go b/a.go\n+" + strings.Repeat("a", 30)
	fileB := "diff --git a/b.go b/b.go\n+" + strings.Repeat("b", 30)
	diff := fileA + "\n" + fileB

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &scriptedReviewer{results: []string{
		`[{"severity":"high","message":"structured finding"}]`,
		"Code phần này trông ổn, không có vấn đề gì.",
	}}

	job := &Job{
		GitHub:            gh,
		Clone:             fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:          reviewer,
		BundleBudgetChars: len(fileA) + 1, // ép chia làm 2 bundle
	}
	job.Run()

	if !gh.editCalled {
		t.Fatal("expected EditComment to be called")
	}
	if !strings.Contains(gh.editedBody, "## 🟣 Yuumi Review") {
		t.Errorf("expected header to still appear (1 bundle did parse), got: %s", gh.editedBody)
	}
	if !strings.Contains(gh.editedBody, "Tổng: 1 góp ý") {
		t.Errorf("expected header to count only the parsed bundle's finding, got: %s", gh.editedBody)
	}
	if !strings.Contains(gh.editedBody, "không tính được vào bảng trên") {
		t.Errorf("expected header to warn the count is partial, got: %s", gh.editedBody)
	}
	if !strings.Contains(gh.editedBody, "Code phần này trông ổn") {
		t.Errorf("expected the raw-text bundle to still be shown in full below the header, got: %s", gh.editedBody)
	}
}

// TestJobRun_UnparsableResult_FallsBackToRawText đảm bảo Job không phá vỡ
// hành vi hiện có khi Claude không tuân theo format JSON (bất chấp hướng
// dẫn trong prompt) — comment vẫn hiển thị nguyên văn text như trước khi có
// issue #26, review không bị coi là lỗi chỉ vì sai định dạng.
func TestJobRun_UnparsableResult_FallsBackToRawText(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+fmt.Println(1)"

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{result: "Code trông ổn, không có vấn đề gì đáng chú ý."}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if !strings.Contains(gh.editedBody, "Code trông ổn, không có vấn đề gì đáng chú ý.") {
		t.Errorf("expected raw text fallback in comment, got: %s", gh.editedBody)
	}
	if strings.Contains(gh.editedBody, "## 🟣 Yuumi Review") {
		t.Errorf("expected no review header when Claude falls back to freeform text (no structured data to summarize), got: %s", gh.editedBody)
	}
}

// wellFormedDiff là 1 diff hunk-parse được (có header "@@ ") để test findings
// gắn được vào dòng thật — khác các diff tối giản "diff --git ...\n+..."
// dùng ở các test khác (chỉ cần đủ cho splitDiffByFile, không cần
// parseFileHunks).
const wellFormedDiff = "diff --git a/main.go b/main.go\n" +
	"--- a/main.go\n" +
	"+++ b/main.go\n" +
	"@@ -1,2 +1,3 @@\n" +
	" package main\n" +
	"+import \"fmt\"\n" +
	" var x = 1"

// TestJobRun_InlineFinding_PostedViaCreateReview_NotDuplicatedInComment xác
// nhận finding có file/line khớp đúng diff thật (issue #5) được post qua
// GitHub Reviews API (CreateReview) — KHÔNG lặp lại nội dung đó trong
// comment tổng hợp (EditComment), tránh người đọc thấy trùng lặp.
func TestJobRun_InlineFinding_PostedViaCreateReview_NotDuplicatedInComment(t *testing.T) {
	rawJSON := `[{"file":"main.go","line":2,"category":"style","severity":"low","message":"unused import fmt"}]`

	gh := &fakeGitHubClient{headSHA: "sha-abc", diff: wellFormedDiff}
	reviewer := &fakeReviewer{result: rawJSON}

	job := &Job{
		GitHub:       gh,
		Clone:        fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:     reviewer,
		RepoFullName: "owner/repo",
		IssueNumber:  7,
	}
	job.Run()

	if len(gh.createReviewCalls) != 1 {
		t.Fatalf("expected CreateReview to be called once, got %d", len(gh.createReviewCalls))
	}
	call := gh.createReviewCalls[0]
	if call.commitSHA != "sha-abc" {
		t.Errorf("CreateReview commitSHA = %q, want %q", call.commitSHA, "sha-abc")
	}
	if !strings.Contains(call.commentsJSON, `"path":"main.go"`) || !strings.Contains(call.commentsJSON, `"line":2`) || !strings.Contains(call.commentsJSON, `"side":"RIGHT"`) {
		t.Errorf("CreateReview commentsJSON = %q, want it to include path/line/side", call.commentsJSON)
	}
	if !strings.Contains(call.commentsJSON, "unused import fmt") {
		t.Errorf("CreateReview commentsJSON = %q, want it to include the finding's message", call.commentsJSON)
	}

	if !gh.editCalled {
		t.Fatal("expected EditComment to be called")
	}
	if strings.Contains(gh.editedBody, "unused import fmt") {
		t.Errorf("expected the inline finding NOT to be duplicated in the summary comment, got: %s", gh.editedBody)
	}
	if !strings.Contains(gh.editedBody, "gắn trực tiếp") {
		t.Errorf("expected the summary comment to note the finding went inline, got: %s", gh.editedBody)
	}
}

// TestJobRun_NoInlineFindings_CreateReviewNotCalled đảm bảo Job không gọi
// GitHub Reviews API khi không có finding nào gắn được vào dòng cụ thể —
// tránh tạo review rỗng/không cần thiết.
func TestJobRun_NoInlineFindings_CreateReviewNotCalled(t *testing.T) {
	rawJSON := `[{"severity":"low","message":"nhận xét tổng quát"}]`

	gh := &fakeGitHubClient{headSHA: "sha-abc", diff: wellFormedDiff}
	reviewer := &fakeReviewer{result: rawJSON}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if len(gh.createReviewCalls) != 0 {
		t.Errorf("expected CreateReview NOT to be called, got %d calls", len(gh.createReviewCalls))
	}
	if !strings.Contains(gh.editedBody, "nhận xét tổng quát") {
		t.Errorf("expected the general finding in the summary comment, got: %s", gh.editedBody)
	}
}

// TestJobRun_CreateReviewFails_DoesNotBlockPrimaryFlow đảm bảo lỗi gọi
// CreateReview (best-effort) không chặn/làm hỏng luồng chính — comment tổng
// hợp đã post thành công vẫn được coi là review thành công (StateStore vẫn
// được lưu, xem issue #21).
func TestJobRun_CreateReviewFails_DoesNotBlockPrimaryFlow(t *testing.T) {
	rawJSON := `[{"file":"main.go","line":2,"category":"style","severity":"low","message":"m"}]`

	gh := &fakeGitHubClient{headSHA: "sha-new", diff: wellFormedDiff, createReviewErr: errors.New("github rate limited")}
	reviewer := &fakeReviewer{result: rawJSON}
	store := newFakeStateStore(nil)

	job := &Job{
		GitHub:       gh,
		Clone:        fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:     reviewer,
		StateStore:   store,
		RepoFullName: "owner/repo",
		IssueNumber:  7,
	}
	job.Run()

	if len(gh.createReviewCalls) != 1 {
		t.Fatalf("expected CreateReview to still be attempted once, got %d", len(gh.createReviewCalls))
	}
	if !store.setCalled {
		t.Error("expected SetLastReviewedSHA to still be called despite CreateReview failing (best-effort)")
	}
}

// TestJobRun_IncrementalReview_ValidatesInlineFindingsAgainstFullDiff đảm
// bảo inline finding được xác thực với diff ĐẦY ĐỦ của PR (base...head, qua
// GetPullRequestDiff), KHÔNG phải diff incremental (compare(lastSHA, sha))
// dùng để build prompt — 2 diff này có thể có hunk khác nhau (issue #21 +
// #5), dùng nhầm diff incremental để validate có thể khiến GitHub từ chối
// cả request (1 review là atomic).
func TestJobRun_IncrementalReview_ValidatesInlineFindingsAgainstFullDiff(t *testing.T) {
	// Diff đầy đủ (base...head) chỉ có hunk cho dòng 1-3 của main.go.
	fullDiff := wellFormedDiff
	// Diff incremental (so với lần review trước) có hunk KHÁC — dòng 5 hợp
	// lệ trong diff incremental nhưng không nằm trong fullDiff ở trên.
	incrementalDiff := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -5,1 +5,1 @@\n" +
		"-old line 5\n" +
		"+new line 5"

	rawJSON := `[{"file":"main.go","line":5,"category":"style","severity":"low","message":"finding on line 5"}]`

	gh := &fakeGitHubClient{
		headSHA:     "sha-new",
		diff:        fullDiff,        // GetPullRequestDiff — dùng để validate
		compareDiff: incrementalDiff, // GetCompareDiff — dùng để build prompt
	}
	reviewer := &fakeReviewer{result: rawJSON}
	store := newFakeStateStore(map[string]string{stateKey("owner/repo", 7): "sha-old"})

	job := &Job{
		GitHub:       gh,
		Clone:        fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:     reviewer,
		StateStore:   store,
		RepoFullName: "owner/repo",
		IssueNumber:  7,
	}
	job.Run()

	if !gh.compareCalled {
		t.Fatal("expected GetCompareDiff to be called (incremental review)")
	}
	if len(gh.createReviewCalls) != 0 {
		t.Errorf("expected CreateReview NOT to be called — line 5 is valid in the incremental diff but not in the full PR diff, got %d calls", len(gh.createReviewCalls))
	}
	if !strings.Contains(gh.editedBody, "finding on line 5") {
		t.Errorf("expected the finding to fall back to the summary comment, got: %s", gh.editedBody)
	}
}

// TestJobRun_IncrementalReview_FullDiffFetchFails_FallsBackToIncrementalDiff
// đảm bảo lỗi lấy diff đầy đủ (để validate) không chặn luồng chính — fallback
// dùng diff incremental để validate, còn hơn không post được inline comment
// nào (best-effort, chấp nhận rủi ro GitHub có thể từ chối 1 vài comment).
func TestJobRun_IncrementalReview_FullDiffFetchFails_FallsBackToIncrementalDiff(t *testing.T) {
	rawJSON := `[{"file":"main.go","line":2,"category":"style","severity":"low","message":"m"}]`

	gh := &fakeGitHubClient{
		headSHA:     "sha-new",
		diffErr:     errors.New("github down"), // GetPullRequestDiff lỗi
		compareDiff: wellFormedDiff,
	}
	reviewer := &fakeReviewer{result: rawJSON}
	store := newFakeStateStore(map[string]string{stateKey("owner/repo", 7): "sha-old"})

	job := &Job{
		GitHub:       gh,
		Clone:        fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:     reviewer,
		StateStore:   store,
		RepoFullName: "owner/repo",
		IssueNumber:  7,
	}
	job.Run()

	if len(gh.createReviewCalls) != 1 {
		t.Fatalf("expected CreateReview to still be called using the incremental diff as fallback, got %d calls", len(gh.createReviewCalls))
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

func TestJobRun_AppliesGitignore(t *testing.T) {
	dir := writeGitignore(t, "generated/\n*.gen.go\n")

	code := "diff --git a/main.go b/main.go\n+fmt.Println(1)"
	generatedDir := "diff --git a/generated/api.go b/generated/api.go\n+package generated"
	generatedExt := "diff --git a/models/user.gen.go b/models/user.gen.go\n+package models"
	diff := strings.Join([]string{code, generatedDir, generatedExt}, "\n")

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner(dir, nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	if !reviewer.called {
		t.Fatal("expected Reviewer.Review to still be called for main.go")
	}
	if strings.Contains(reviewer.gotPrompt, "generated/api.go") || strings.Contains(reviewer.gotPrompt, "user.gen.go") {
		t.Errorf("expected files matching .gitignore to be excluded, got prompt:\n%s", reviewer.gotPrompt)
	}
	if !strings.Contains(gh.editedBody, "Đã bỏ qua") {
		t.Errorf("expected comment to note skipped files, got: %s", gh.editedBody)
	}
}

func TestJobRun_CombinesRepoConfigAndGitignoreIgnorePatterns(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, repoConfigFileName), []byte("exclude:\n  - \"testdata/\"\n"), 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", repoConfigFileName, err)
	}
	if err := os.WriteFile(filepath.Join(dir, gitignoreFileName), []byte("coverage/\n"), 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", gitignoreFileName, err)
	}

	code := "diff --git a/main.go b/main.go\n+fmt.Println(1)"
	fromRepoConfig := "diff --git a/testdata/case1.json b/testdata/case1.json\n+{}"
	fromGitignore := "diff --git a/coverage/report.out b/coverage/report.out\n+mode: set"
	diff := strings.Join([]string{code, fromRepoConfig, fromGitignore}, "\n")

	gh := &fakeGitHubClient{headSHA: "abc123", diff: diff}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner(dir, nil, new(bool)),
		Reviewer: reviewer,
	}
	job.Run()

	// Cả 2 nguồn (.yuumi.yml exclude + .gitignore) phải cộng dồn, không cái
	// nào lấn át cái nào.
	if strings.Contains(reviewer.gotPrompt, "testdata") {
		t.Errorf("expected .yuumi.yml exclude to still apply, got prompt:\n%s", reviewer.gotPrompt)
	}
	if strings.Contains(reviewer.gotPrompt, "coverage/report.out") {
		t.Errorf("expected .gitignore pattern to also apply, got prompt:\n%s", reviewer.gotPrompt)
	}
	if !strings.Contains(reviewer.gotPrompt, "main.go") {
		t.Errorf("expected main.go to still be reviewed, got prompt:\n%s", reviewer.gotPrompt)
	}
}

func TestJobRun_NoStateStore_UsesFullDiff(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/main.go b/main.go\n+fmt.Println(1)"}
	reviewer := &fakeReviewer{result: "trông ổn"}

	job := &Job{
		GitHub:   gh,
		Clone:    fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer: reviewer,
		// StateStore không set (nil) — hành vi phải giống hệt trước issue #21.
	}
	job.Run()

	if gh.compareCalled {
		t.Error("GetCompareDiff should not be called when StateStore is nil")
	}
	if !strings.Contains(reviewer.gotPrompt, "main.go") {
		t.Errorf("expected full diff to be reviewed, got prompt:\n%s", reviewer.gotPrompt)
	}
}

func TestJobRun_NoPriorReview_UsesFullDiff(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "abc123", diff: "diff --git a/main.go b/main.go\n+fmt.Println(1)"}
	reviewer := &fakeReviewer{result: "trông ổn"}
	store := newFakeStateStore(nil) // chưa có PR nào được review trước đó

	job := &Job{
		GitHub:     gh,
		Clone:      fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:   reviewer,
		StateStore: store,
	}
	job.Run()

	if gh.compareCalled {
		t.Error("GetCompareDiff should not be called on the first review of a PR")
	}
	if !strings.Contains(reviewer.gotPrompt, "main.go") {
		t.Errorf("expected full diff to be reviewed, got prompt:\n%s", reviewer.gotPrompt)
	}
}

func TestJobRun_PriorReviewFound_UsesCompareDiff(t *testing.T) {
	gh := &fakeGitHubClient{
		headSHA:     "sha-new",
		diff:        "diff --git a/main.go b/main.go\n+everything from scratch",
		compareDiff: "diff --git a/main.go b/main.go\n+only the new part",
	}
	reviewer := &fakeReviewer{result: "trông ổn"}
	store := newFakeStateStore(map[string]string{stateKey("owner/repo", 7): "sha-old"})

	job := &Job{
		GitHub:       gh,
		Clone:        fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:     reviewer,
		StateStore:   store,
		RepoFullName: "owner/repo",
		IssueNumber:  7,
	}
	job.Run()

	if !gh.compareCalled {
		t.Fatal("expected GetCompareDiff to be called when a prior review exists")
	}
	if gh.gotCompareBase != "sha-old" || gh.gotCompareHead != "sha-new" {
		t.Errorf("GetCompareDiff called with (%q, %q), want (%q, %q)", gh.gotCompareBase, gh.gotCompareHead, "sha-old", "sha-new")
	}
	if !strings.Contains(reviewer.gotPrompt, "only the new part") {
		t.Errorf("expected prompt to use the compare diff, got:\n%s", reviewer.gotPrompt)
	}
	if strings.Contains(reviewer.gotPrompt, "everything from scratch") {
		t.Errorf("expected prompt NOT to contain the full PR diff, got:\n%s", reviewer.gotPrompt)
	}
	if !strings.Contains(gh.editedBody, "Chỉ review phần thay đổi mới") {
		t.Errorf("expected comment to note incremental review, got: %s", gh.editedBody)
	}
}

func TestJobRun_CompareDiffError_FallsBackToFullDiff(t *testing.T) {
	gh := &fakeGitHubClient{
		headSHA:        "sha-new",
		diff:           "diff --git a/main.go b/main.go\n+full diff fallback",
		compareDiffErr: errors.New("github api error"),
	}
	reviewer := &fakeReviewer{result: "trông ổn"}
	store := newFakeStateStore(map[string]string{stateKey("owner/repo", 7): "sha-old"})

	job := &Job{
		GitHub:       gh,
		Clone:        fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:     reviewer,
		StateStore:   store,
		RepoFullName: "owner/repo",
		IssueNumber:  7,
	}
	job.Run()

	if !strings.Contains(reviewer.gotPrompt, "full diff fallback") {
		t.Errorf("expected fallback to full diff on compare error, got prompt:\n%s", reviewer.gotPrompt)
	}
}

func TestJobRun_LastReviewedSHALookupError_FallsBackToFullDiff(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "sha-new", diff: "diff --git a/main.go b/main.go\n+full diff fallback"}
	reviewer := &fakeReviewer{result: "trông ổn"}
	store := newFakeStateStore(nil)
	store.lastSHAErr = errors.New("state file corrupted")

	job := &Job{
		GitHub:     gh,
		Clone:      fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:   reviewer,
		StateStore: store,
	}
	job.Run()

	if gh.compareCalled {
		t.Error("GetCompareDiff should not be called when the state lookup itself errors")
	}
	if !strings.Contains(reviewer.gotPrompt, "full diff fallback") {
		t.Errorf("expected fallback to full diff on state lookup error, got prompt:\n%s", reviewer.gotPrompt)
	}
}

func TestJobRun_SuccessfulReview_SavesLastReviewedSHA(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "sha-new", diff: "diff --git a/main.go b/main.go\n+fmt.Println(1)"}
	reviewer := &fakeReviewer{result: "trông ổn"}
	store := newFakeStateStore(nil)

	job := &Job{
		GitHub:       gh,
		Clone:        fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:     reviewer,
		StateStore:   store,
		RepoFullName: "owner/repo",
		IssueNumber:  7,
	}
	job.Run()

	if !store.setCalled {
		t.Fatal("expected SetLastReviewedSHA to be called after a successful review")
	}
	if store.gotSetSHA != "sha-new" {
		t.Errorf("SetLastReviewedSHA called with %q, want %q", store.gotSetSHA, "sha-new")
	}
}

func TestJobRun_ReviewerError_DoesNotSaveLastReviewedSHA(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "sha-new", diff: "diff --git a/main.go b/main.go\n+fmt.Println(1)"}
	reviewer := &fakeReviewer{err: errors.New("claude CLI timeout")}
	store := newFakeStateStore(nil)

	job := &Job{
		GitHub:       gh,
		Clone:        fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:     reviewer,
		StateStore:   store,
		RepoFullName: "owner/repo",
		IssueNumber:  7,
	}
	job.Run()

	if !gh.editCalled {
		t.Fatal("expected EditComment to still be called with the failure message")
	}
	if store.setCalled {
		t.Error("expected SetLastReviewedSHA NOT to be called when the review itself failed")
	}
}

func TestJobRun_EditCommentError_DoesNotSaveLastReviewedSHA(t *testing.T) {
	gh := &fakeGitHubClient{headSHA: "sha-new", diff: "diff --git a/main.go b/main.go\n+fmt.Println(1)", editErr: errors.New("network error")}
	reviewer := &fakeReviewer{result: "trông ổn"}
	store := newFakeStateStore(nil)

	job := &Job{
		GitHub:       gh,
		Clone:        fakeCloner("/tmp/fake-dir", nil, new(bool)),
		Reviewer:     reviewer,
		StateStore:   store,
		RepoFullName: "owner/repo",
		IssueNumber:  7,
	}
	job.Run()

	if store.setCalled {
		t.Error("expected SetLastReviewedSHA NOT to be called when posting the comment itself failed")
	}
}
