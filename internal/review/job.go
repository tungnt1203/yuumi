package review

import (
	"fmt"
	"strings"
	"time"
)

// defaultBundleBudgetChars là ngưỡng mặc định (tính theo số ký tự) cho mỗi
// bundle khi Job.BundleBudgetChars không được set (<=0). Ký tự là proxy rẻ
// và đủ tốt cho số token thực tế — không cần tokenizer chính xác ở đây.
// Con số này là điểm khởi đầu hợp lý, cần tinh chỉnh lại bằng PR lớn thật
// (xem issue #3) — vì vậy nó KHÔNG phải const cứng mà override được qua
// Job.BundleBudgetChars (và Config.MaxDiffBundleChars ở tầng main.go).
const defaultBundleBudgetChars = 12_000

// GitHubClient là tập con các method của githubapi.Client mà Job cần.
// Khai báo interface riêng ở đây (thay vì phụ thuộc thẳng *githubapi.Client)
// để test Job.Run bằng fake, không phải gọi API GitHub thật.
type GitHubClient interface {
	GetPullRequestHeadSHA(repoFullName string, pullRequestNumber int) (string, error)
	GetPullRequestDiff(repoFullName string, pullRequestNumber int) (string, error)
	GetCompareDiff(repoFullName string, baseSHA string, headSHA string) (string, error)
	GetPullRequestChangedFilesCount(repoFullName string, pullRequestNumber int) (int, error)
	EditComment(repoFullName string, commentID int64, body string) error
}

// Cloner khớp chữ ký gitrepo.CloneRepo — khai báo dạng func type để Job có
// thể nhận vào gitrepo.CloneRepo (production) hoặc 1 fake (test) mà không
// cần Job biết đến package gitrepo.
type Cloner func(repoFullName string, sha string) (dir string, cleanup func(), err error)

// ReviewLogger ghi lại chi tiết 1 lần gọi Reviewer.Review — prompt gửi đi,
// response nhận được (rỗng nếu lỗi), lỗi (nếu có) và thời gian xử lý — để
// trace lại được đã gửi/nhận gì khi cần debug review lỗi ở production (xem
// issue #9). Khai báo interface riêng dùng toàn tham số cơ bản (không phải
// struct từ package ghi log cụ thể), theo đúng cách GitHubClient/Cloner đã
// tách ở trên — Job không cần biết log được lưu vào đâu (file, DB...).
//
// bundleIndex/bundleTotal đánh số từ 1, dùng để phân biệt các bundle khi 1
// PR lớn bị chia nhiều phần (xem bundleDiffs). Logger là optional: Job.Logger
// == nil nghĩa là "không ghi log", Run() vẫn hoạt động bình thường.
//
// attempts là số lần Reviewer.Review thực sự tốn để ra được kết quả/lỗi
// cuối (xem review.Reviewer, issue #28) — 1 nghĩa là không phải retry.
// numTurns là số turn model dùng ở lần gọi cuối (xem review.Reviewer, issue
// #20) — tín hiệu rẻ để biết model có thực sự đọc thêm file ngoài diff.
type ReviewLogger interface {
	LogReview(repoFullName string, issueNumber int, sha string, bundleIndex, bundleTotal int, prompt, response, errMsg string, duration time.Duration, attempts int, numTurns int)
}

// ReviewStateStore tra cứu/lưu lại SHA đã review lần gần nhất cho 1 (repo,
// PR) cụ thể, để Run() chỉ gửi diff phần thay đổi MỚI khi PR đã được review
// trước đó, thay vì gửi lại toàn bộ diff so với base mỗi lần review thêm
// (xem loadDiff, issue #21). Khai báo interface riêng theo đúng cách
// GitHubClient/Cloner/ReviewLogger đã tách ở trên, để test bằng fake và để
// Job không cần biết state lưu ở đâu (file, DB...).
//
// nil (Job.StateStore == nil) nghĩa là "chưa cấu hình tính năng này" —
// Run() luôn lấy full diff so với base, giống hành vi trước issue #21.
type ReviewStateStore interface {
	LastReviewedSHA(repoFullName string, issueNumber int) (sha string, found bool, err error)
	SetLastReviewedSHA(repoFullName string, issueNumber int, sha string) error
}

// Job đóng gói toàn bộ dữ liệu cần để thực hiện 1 lần review (clone repo,
// lấy diff, gọi Reviewer, sửa lại comment placeholder). Tách ra khỏi
// main.go để nơi nhận webhook (main.go) không cần biết chi tiết các bước
// bên trong — chỉ cần dựng Job rồi chạy go job.Run().
type Job struct {
	GitHub        GitHubClient
	Clone         Cloner
	Reviewer      Reviewer
	RepoFullName  string
	IssueNumber   int
	PlaceholderID int64
	UserCommand   string

	// BundleBudgetChars giới hạn kích thước (ký tự) diff gửi trong 1 lần gọi
	// Reviewer.Review. Diff PR vượt ngưỡng này sẽ bị chia thành nhiều bundle,
	// mỗi bundle review riêng rồi gộp kết quả (xem bundleDiffs). <=0 nghĩa là
	// "chưa cấu hình" — dùng defaultBundleBudgetChars.
	BundleBudgetChars int

	// Logger ghi lại prompt/response/lỗi/thời gian xử lý của mỗi lần gọi
	// Reviewer.Review (xem ReviewLogger, issue #9). nil nghĩa là "không ghi
	// log" — Run() vẫn chạy bình thường, không bắt buộc phải cấu hình.
	Logger ReviewLogger

	// StateStore tra cứu/lưu SHA đã review lần gần nhất cho PR này (xem
	// ReviewStateStore, issue #21). nil nghĩa là "không bật tính năng" —
	// Run() luôn lấy full diff so với base, không tối ưu diff lần review
	// thêm.
	StateStore ReviewStateStore
}

// Run thực hiện review, nên luôn được gọi trong goroutine riêng
// (vd `go job.Run()`) vì có thể chạy lâu (clone repo, gọi Claude CLI).
//
// Diff PR quá lớn được chia thành nhiều bundle (xem bundleDiffs), mỗi bundle
// review riêng rồi gộp kết quả trước khi edit lại đúng 1 comment placeholder
// (issue #3) — với PR bình thường (đa số), bundleDiffs trả về đúng 1 bundle
// chứa nguyên diff, hành vi giống hệt trước khi có bundling.
func (j *Job) Run() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered from panic:", r)
		}
	}()

	sha, err := j.GitHub.GetPullRequestHeadSHA(j.RepoFullName, j.IssueNumber)
	if err != nil {
		fmt.Println("Get pull request head SHA error:", err)
		return
	}

	dir, cleanup, err := j.Clone(j.RepoFullName, sha)
	if err != nil {
		fmt.Println("Clone repo error:", err)
		return
	}
	defer cleanup()

	// Chạy 1 lần cho cả PR (không phụ thuộc bundle nào) — kết quả gofmt/go
	// vet là thuộc tính của code sau khi đổi, không phải của từng phần diff
	// bị chia nhỏ (xem issue #8).
	staticReport := staticCheckReport(dir)

	// Cấu hình riêng của repo (issue #7) — không có file .yuumi.yml (đa số
	// repo) hay parse lỗi đều không chặn review, chỉ log rồi dùng default.
	repoCfg, err := loadRepoConfig(dir)
	if err != nil {
		fmt.Println("Load .yuumi.yml error (dùng default):", err)
	}

	// .gitignore thật của repo (issue #27) — cộng dồn thêm vào ignore pattern
	// của .yuumi.yml/default, KHÔNG thay thế. Không có file hay đọc lỗi đều
	// không chặn review, chỉ log rồi bỏ qua — nhất quán với .yuumi.yml.
	gitignorePatterns, err := loadGitignorePatterns(dir)
	if err != nil {
		fmt.Println("Load .gitignore error (bỏ qua):", err)
	}
	extraIgnoredPatterns := append(append([]string{}, repoCfg.Exclude...), gitignorePatterns...)

	diff, incremental, err := j.loadDiff(sha)
	if err != nil {
		// Không chặn review nếu lấy diff lỗi — fallback về cách cũ
		// (Claude tự đọc file state + commit message).
		fmt.Println("Get pull request diff error:", err)
	}

	var notes []string
	if incremental {
		// PR này đã được review trước đó (issue #21) — diff gửi đi chỉ là
		// phần thay đổi mới, không phải toàn bộ PR, nên changed_files của cả
		// PR (diffTruncationWarning) không áp dụng được ở đây: gần như luôn
		// lệch (incremental luôn có ít file hơn cả PR) dù không có gì bị cắt
		// thật. Báo rõ cho người đọc biết bot có tối ưu, tránh hiểu nhầm
		// review sót phần cũ.
		notes = append(notes, "_(Chỉ review phần thay đổi mới so với lần review trước, không phải toàn bộ PR.)_")
	} else if strings.TrimSpace(diff) != "" {
		if note := j.diffTruncationWarning(diff); note != "" {
			notes = append(notes, note)
		}
	}

	budget := j.BundleBudgetChars
	if budget <= 0 {
		budget = defaultBundleBudgetChars
	}

	bundles, skipped := bundleDiffs(diff, budget, extraIgnoredPatterns)
	if len(skipped) > 0 {
		notes = append(notes, skippedNote(skipped))
	}

	var merged string
	var hadError bool
	switch {
	case len(bundles) == 0 && len(skipped) > 0:
		// Diff CÓ nội dung nhưng toàn bộ file đều bị lọc (vd PR chỉ sửa
		// go.sum) — không có gì đáng review, không tốn 1 lần gọi Claude CLI
		// chỉ để nó nói "không có gì để xem".
		merged = "Không có file nào cần review."
	case len(bundles) == 0:
		// Diff rỗng thật (GetPullRequestDiff lỗi ở trên, hoặc PR không đổi
		// gì) — vẫn review 1 lần với diff rỗng, để BuildReviewPrompt tự
		// chèn hướng dẫn fallback (Claude tự đọc file state + commit message).
		merged, hadError = j.reviewBundles([]string{""}, dir, sha, staticReport, repoCfg.Instructions)
	default:
		merged, hadError = j.reviewBundles(bundles, dir, sha, staticReport, repoCfg.Instructions)
	}

	if len(notes) > 0 {
		merged = strings.Join(notes, "\n") + "\n\n" + merged
	}

	if err := j.GitHub.EditComment(j.RepoFullName, j.PlaceholderID, merged); err != nil {
		fmt.Println("Post comment error:", err)
		return
	}
	fmt.Println("Comment posted successfully")

	// Chỉ ghi nhận "đã review tới SHA này" nếu KHÔNG có bundle nào lỗi —
	// review thất bại (vd Claude CLI lỗi) không nên coi là đã xem qua code ở
	// SHA đó, nếu không lần review kế (issue #21) sẽ bỏ sót đúng phần lẽ ra
	// cần xem lại.
	if !hadError && j.StateStore != nil {
		if err := j.StateStore.SetLastReviewedSHA(j.RepoFullName, j.IssueNumber, sha); err != nil {
			fmt.Println("Save last reviewed SHA error:", err)
		}
	}
}

// loadDiff quyết định lấy diff nào để review. Nếu StateStore có SHA đã
// review lần trước cho đúng PR này, chỉ lấy phần thay đổi MỚI từ SHA đó
// tới SHA hiện tại qua GetCompareDiff (incremental == true) — tránh gửi
// lại toàn bộ diff cũ mỗi lần review thêm 1 PR đã review rồi (issue #21).
//
// Không tìm thấy lần review trước (lần đầu review PR này, StateStore chưa
// cấu hình, hoặc tra cứu/GetCompareDiff lỗi) → fallback về hành vi cũ: lấy
// full diff so với base qua GetPullRequestDiff, incremental == false.
func (j *Job) loadDiff(sha string) (diff string, incremental bool, err error) {
	if j.StateStore != nil {
		lastSHA, found, stateErr := j.StateStore.LastReviewedSHA(j.RepoFullName, j.IssueNumber)
		if stateErr != nil {
			fmt.Println("Load last reviewed SHA error (dùng full diff):", stateErr)
		} else if found {
			compareDiff, compareErr := j.GitHub.GetCompareDiff(j.RepoFullName, lastSHA, sha)
			if compareErr != nil {
				fmt.Println("Get compare diff error (fallback full diff):", compareErr)
			} else {
				return compareDiff, true, nil
			}
		}
	}

	fullDiff, err := j.GitHub.GetPullRequestDiff(j.RepoFullName, j.IssueNumber)
	return fullDiff, false, err
}

// diffTruncationWarning so số file parse được từ diff với "changed_files" mà
// GitHub báo cáo cho cả PR (qua GetPullRequestChangedFilesCount) — nếu lệch,
// nhiều khả năng GitHub đã tự giới hạn/cắt bớt diff trả về (PR quá nhiều
// file thay đổi), và review có thể âm thầm sót file nếu không cảnh báo.
// Đếm số file TRƯỚC khi lọc file rác ở bundleDiffs (Việc 3 của issue #3) —
// lọc là chủ ý của mình, không phải do GitHub cắt, không được tính nhầm.
//
// Lỗi khi gọi GetPullRequestChangedFilesCount không chặn review, chỉ bỏ
// qua việc so sánh — nhất quán với cách lỗi GetPullRequestDiff cũng không
// chặn review (xem Run).
func (j *Job) diffTruncationWarning(diff string) string {
	changedFiles, err := j.GitHub.GetPullRequestChangedFilesCount(j.RepoFullName, j.IssueNumber)
	if err != nil {
		fmt.Println("Get pull request changed files count error:", err)
		return ""
	}

	parsed := len(splitDiffByFile(diff))
	if parsed >= changedFiles {
		return ""
	}
	return fmt.Sprintf("⚠️ GitHub chỉ trả về diff của %d/%d file — review có thể sót file.", parsed, changedFiles)
}

// reviewBundles chạy Reviewer.Review tuần tự cho từng bundle rồi gộp kết
// quả thành 1 văn bản duy nhất (EditComment chỉ sửa được đúng 1 comment
// placeholder — xem PlaceholderID).
//
// Chạy tuần tự thay vì song song: mỗi bundle là 1 tiến trình `claude` riêng
// trong cùng thư mục repo đã clone, và review PR lớn vốn đã là đường hiếm
// gặp/không chặn HTTP response (Job chạy trong goroutine nền) — đơn giản và
// dễ đoán quan trọng hơn là tối ưu vài chục giây. Có thể revisit sau nếu
// thực tế thấy quá chậm.
//
// Lỗi ở 1 bundle không làm hỏng cả kết quả: bundle đó được ghi nhận lỗi
// trong phần của nó, các bundle còn lại vẫn tiếp tục — PR lớn mà chỉ vì 1
// phần bị lỗi (vd timeout) mà mất luôn kết quả của các phần đã review xong
// thì phí hơn nhiều so với review PR nhỏ.
//
// hadError báo có ÍT NHẤT 1 bundle lỗi không — Run() dùng để quyết định có
// nên ghi nhận "đã review xong tới SHA này" vào StateStore không (issue
// #21): review lỗi không nên tính là đã xem qua code ở SHA đó.
func (j *Job) reviewBundles(bundles []string, dir string, sha string, staticReport string, repoInstructions string) (merged string, hadError bool) {
	single := len(bundles) == 1
	sections := make([]string, len(bundles))

	for i, bundleDiff := range bundles {
		promptDiff := bundleDiff
		if !single && bundleDiff != "" {
			promptDiff = bundleNote(i+1, len(bundles)) + bundleDiff
		}

		prompt := BuildReviewPrompt(j.UserCommand, promptDiff, staticReport, repoInstructions)
		start := time.Now()
		text, attempts, numTurns, err := j.Reviewer.Review(prompt, dir)
		duration := time.Since(start)

		errMsg := ""
		if err != nil {
			fmt.Println("Review bundle", i+1, "/", len(bundles), "error:", err)
			errMsg = err.Error()
			text = "❌ Review thất bại: " + errMsg
			hadError = true
		} else {
			fmt.Println("Review bundle", i+1, "/", len(bundles), "result:", text)
		}

		if j.Logger != nil {
			// Log response gốc (rỗng nếu lỗi), không phải text đã bọc thêm
			// "❌ Review thất bại: ..." — để file log phản ánh đúng những gì
			// Reviewer thực sự trả về.
			response := text
			if err != nil {
				response = ""
			}
			j.Logger.LogReview(j.RepoFullName, j.IssueNumber, sha, i+1, len(bundles), prompt, response, errMsg, duration, attempts, numTurns)
		}

		if single {
			sections[i] = text
		} else {
			sections[i] = fmt.Sprintf("### Phần %d/%d\n%s", i+1, len(bundles), text)
		}
	}

	return strings.Join(sections, "\n\n"), hadError
}

// skippedNote render 1 dòng thông báo các file bị bundleDiffs bỏ qua
// (defaultIgnoredPathPatterns) — để người review biết bot có chủ đích
// không xem các file này, thay vì im lặng bỏ sót. Cap hiển thị tối đa
// showLimit tên để không làm phình comment nếu PR đổi rất nhiều file bị lọc
// (vd đổi cả cây vendor/).
func skippedNote(skipped []string) string {
	const showLimit = 10

	shown := skipped
	suffix := ""
	if len(skipped) > showLimit {
		shown = skipped[:showLimit]
		suffix = fmt.Sprintf(" và %d file khác", len(skipped)-showLimit)
	}
	return fmt.Sprintf("_(Đã bỏ qua %d file không cần review: %s%s)_", len(skipped), strings.Join(shown, ", "), suffix)
}
