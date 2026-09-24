package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
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
//
// CreateReview nhận comments đã marshal sẵn thành JSON ([]byte, dạng mảng
// object {"path","line","side","body"}, thêm "start_line"/"start_side" khi
// suggestion thay một khoảng dòng) thay vì 1 struct type riêng — để
// GitHubClient tiếp tục chỉ dùng tham số cơ bản (string/int/[]byte) như mọi
// method khác ở đây, GitHubClient (và implementation githubapi.Client)
// không cần biết/import bất cứ gì về Finding hay pendingComment (xem
// postInlineComments, issue #5).
type GitHubClient interface {
	GetPullRequestHeadSHA(repoFullName string, pullRequestNumber int) (string, error)
	GetPullRequestDiff(repoFullName string, pullRequestNumber int) (string, error)
	GetCompareDiff(repoFullName string, baseSHA string, headSHA string) (string, error)
	GetPullRequestChangedFilesCount(repoFullName string, pullRequestNumber int) (int, error)
	EditComment(repoFullName string, commentID int64, body string) error
	CreateReview(repoFullName string, pullRequestNumber int, commitSHA string, body string, commentsJSON []byte) error
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
// stats là CallStats mà Reviewer.Review trả về cho lần gọi đó.
type ReviewLogger interface {
	LogReview(repoFullName string, issueNumber int, sha string, bundleIndex, bundleTotal int, prompt, response, errMsg string, duration time.Duration, stats CallStats)
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

// BundleCache lưu kết quả của từng bundle đã review xong, để lần chạy lại
// sau khi review bị ngắt giữa chừng (bundle lỗi, server restart...) bỏ qua
// các bundle đã có kết quả (issue #76). key tính từ prompt (xem
// bundleCacheKey), nên chỉ khớp khi prompt giống hệt: đổi diff, hướng dẫn
// repo hay lệnh của người review đều thành key mới.
//
// nil (Job.BundleCache == nil) nghĩa là không bật — mọi bundle luôn gọi
// Reviewer như trước.
type BundleCache interface {
	LoadBundle(repoFullName string, issueNumber int, key string) (text string, found bool, err error)
	SaveBundle(repoFullName string, issueNumber int, key, text string) error
	ClearBundles(repoFullName string, issueNumber int) error
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

	// BundleCache lưu kết quả từng bundle để resume review bị ngắt (xem
	// BundleCache, issue #76). nil nghĩa là không bật.
	BundleCache BundleCache
}

// Run thực hiện review, nên luôn được gọi trong goroutine riêng
// (vd `go job.Run()`) vì có thể chạy lâu (clone repo, gọi Claude CLI).
//
// Diff PR quá lớn được chia thành nhiều bundle (xem bundleDiffs), mỗi bundle
// review riêng rồi gộp kết quả trước khi edit lại đúng 1 comment placeholder
// (issue #3) — với PR bình thường (đa số), bundleDiffs trả về đúng 1 bundle
// chứa nguyên diff, hành vi giống hệt trước khi có bundling.
func (j *Job) Run() {
	// posted ngăn recover ghi đè comment đã post thành công. Panic sau
	// EditComment (inline comment, lưu SHA) không được đổi kết quả review
	// đã hiện trên PR thành dòng thất bại.
	var posted bool
	defer func() {
		if r := recover(); r != nil {
			if posted {
				fmt.Println("Recovered from panic:", r)
				return
			}
			j.reportFailure(fmt.Errorf("panic: %v", r))
		}
	}()

	sha, err := j.GitHub.GetPullRequestHeadSHA(j.RepoFullName, j.IssueNumber)
	if err != nil {
		j.reportFailure(fmt.Errorf("không lấy được head SHA: %w", err))
		return
	}

	dir, cleanup, err := j.Clone(j.RepoFullName, sha)
	if err != nil {
		j.reportFailure(fmt.Errorf("không clone được repo: %w", err))
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
	// validationDiff là diff dùng để XÁC THỰC finding.File/Line trước khi
	// post inline comment (splitFindingsForPosting) — PHẢI là diff đầy đủ
	// của cả PR (base...head), vì đó chính xác là diff GitHub Reviews API
	// đối chiếu khi nhận 1 comment gắn vào dòng, không phải diff tuỳ ý nào
	// khác. Review không incremental thì `diff` đã chính là diff đầy đủ
	// này — dùng luôn, không tốn thêm lệnh gọi.
	validationDiff := diff
	if incremental {
		// PR này đã được review trước đó (issue #21) — diff gửi đi (và
		// dùng để bundle/build prompt) chỉ là phần thay đổi MỚI so với lần
		// review trước (compare(lastSHA, sha)), không phải toàn bộ PR, nên:
		//   - changed_files của cả PR (diffTruncationWarning) không áp dụng
		//     được ở đây: gần như luôn lệch (incremental luôn có ít file
		//     hơn cả PR) dù không có gì bị cắt thật.
		//   - hunk của diff incremental có thể KHÁC hunk GitHub tính cho
		//     base...head (vd 1 dòng context nằm gần thay đổi so với lần
		//     review trước, nhưng lại xa mọi thay đổi so với base) — dùng
		//     nhầm diff này để validate inline comment có thể khiến GitHub
		//     từ chối cả request (1 review là atomic — sai 1 comment mất
		//     luôn TẤT CẢ), nên phải lấy riêng diff đầy đủ chỉ để validate.
		notes = append(notes, "_(Chỉ review phần thay đổi mới so với lần review trước, không phải toàn bộ PR.)_")

		fullDiff, ferr := j.GitHub.GetPullRequestDiff(j.RepoFullName, j.IssueNumber)
		if ferr != nil {
			// Best-effort: fallback dùng diff incremental để validate (rủi
			// ro bug nói trên) còn hơn không post được inline comment nào —
			// không tệ hơn hành vi trước khi có fix này.
			fmt.Println("Get full PR diff for inline comment validation error (dùng diff incremental, 1 số comment hợp lệ có thể bị GitHub từ chối):", ferr)
		} else {
			validationDiff = fullDiff
		}
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

	// primer chỉ đáng tổng hợp khi PR THẬT SỰ bị chia nhiều bundle — PR bình
	// thường (1 bundle, đại đa số) đã thấy nguyên diff của mình rồi, primer
	// liệt kê lại đúng những file nó đang thấy không mang thêm giá trị gì
	// (issue #18).
	var primer string
	if len(bundles) > 1 {
		primer = buildPrimer(dir, changedFilePaths(diff, extraIgnoredPatterns))
	}

	var merged string
	var hadError bool
	var inline []pendingComment
	var header string
	if len(bundles) == 0 && len(skipped) > 0 {
		// Diff CÓ nội dung nhưng toàn bộ file đều bị lọc (vd PR chỉ sửa
		// go.sum) — không có gì đáng review, không tốn 1 lần gọi Claude CLI
		// chỉ để nó nói "không có gì để xem". Không có finding nào để tổng
		// hợp nên bỏ qua header luôn, tránh 1 banner "0 góp ý" thừa thãi.
		merged = "Không có file nào cần review."
	} else {
		// effectiveBundles: dùng đúng `bundles` bình thường, trừ trường hợp
		// diff rỗng thật (GetPullRequestDiff lỗi ở trên, hoặc PR không đổi
		// gì) thì vẫn cần review 1 lần với diff rỗng, để BuildReviewPrompt
		// tự chèn hướng dẫn fallback (Claude tự đọc file state + commit
		// message) — gộp 2 case cũ (len(bundles)==0 và bình thường) làm 1
		// để logic tính header dưới đây không bị lặp lại y hệt ở 2 nơi.
		effectiveBundles := bundles
		if len(effectiveBundles) == 0 {
			effectiveBundles = []string{""}
		}

		var allFindings []Finding
		var anyParsed, allParsed bool
		merged, hadError, inline, allFindings, anyParsed, allParsed = j.reviewBundles(effectiveBundles, dir, sha, staticReport, repoCfg.Instructions, validationDiff, primer)
		if anyParsed {
			// partial=true khi có bundle KHÔNG đóng góp được vào allFindings
			// (lỗi hoặc Claude trả văn xuôi tự do) — renderReviewHeader cần
			// biết để cảnh báo "Tổng: N" chỉ tính phần parse được, tránh
			// người đọc tưởng N là toàn bộ vấn đề của PR trong khi nội dung
			// raw-text bên dưới có thể còn thêm vấn đề chưa được đếm.
			header = renderReviewHeader(allFindings, anyParsed && !allParsed)
		}
	}

	// Thứ tự hiển thị: header tổng quan (nếu có) trước tiên, rồi tới các
	// ghi chú meta (incremental review, file bị bỏ qua, diff bị cắt...),
	// cuối cùng mới tới nội dung review chi tiết — giống bố cục 1 báo cáo
	// review điển hình: tóm tắt trước, chi tiết sau.
	var parts []string
	if header != "" {
		parts = append(parts, header)
	}
	if len(notes) > 0 {
		parts = append(parts, strings.Join(notes, "\n"))
	}
	parts = append(parts, merged)
	merged = strings.Join(parts, "\n\n")

	if err := j.GitHub.EditComment(j.RepoFullName, j.PlaceholderID, merged); err != nil {
		fmt.Println("Post comment error:", err)
		return
	}
	posted = true
	fmt.Println("Comment posted successfully")

	if len(inline) > 0 {
		// Best-effort, KHÔNG return/chặn gì nếu lỗi — comment tổng hợp
		// (quan trọng hơn) đã post thành công ở trên; inline comment chỉ là
		// bổ sung, mất nó (vd rate limit, lỗi mạng) không nên làm mất luôn
		// kết quả review đã có (issue #5).
		if err := j.postInlineComments(sha, inline); err != nil {
			fmt.Println("Post inline review comments error:", err)
		}
	}

	// Chỉ ghi nhận "đã review tới SHA này" nếu KHÔNG có bundle nào lỗi —
	// review thất bại (vd Claude CLI lỗi) không nên coi là đã xem qua code ở
	// SHA đó, nếu không lần review kế (issue #21) sẽ bỏ sót đúng phần lẽ ra
	// cần xem lại.
	if !hadError && j.StateStore != nil {
		if err := j.StateStore.SetLastReviewedSHA(j.RepoFullName, j.IssueNumber, sha); err != nil {
			fmt.Println("Save last reviewed SHA error:", err)
		}
	}
	// Review đã xong trọn vẹn và đã post: không còn gì để resume.
	if !hadError && j.BundleCache != nil {
		if err := j.BundleCache.ClearBundles(j.RepoFullName, j.IssueNumber); err != nil {
			fmt.Println("Clear bundle cache error:", err)
		}
	}
}

// reviewSetupFailureComment là body duy nhất được post khi review fail trước
// lúc có kết quả. err.Error() chỉ được in ra log server: lỗi GitHub, clone
// và panic có thể chứa đường dẫn máy hoặc token (clone URL nhúng token khi
// hỗ trợ repo private, issue #48).
const reviewSetupFailureComment = "❌ Review thất bại — xem log server để biết chi tiết."

// reportFailure ghi một câu chung lên comment placeholder thay vì để
// "Đang review..." treo, và in err đầy đủ ra log. Dùng cho lỗi trước khi
// review chạy xong (head SHA, clone, panic). Lỗi EditComment chỉ được log:
// không còn comment nào khác để báo.
func (j *Job) reportFailure(err error) {
	fmt.Printf("Job failed: repo=%s pr=%d comment=%d err=%v\n", j.RepoFullName, j.IssueNumber, j.PlaceholderID, err)
	if j.GitHub == nil {
		return
	}
	if editErr := j.GitHub.EditComment(j.RepoFullName, j.PlaceholderID, reviewSetupFailureComment); editErr != nil {
		fmt.Printf("Edit comment with failure error: repo=%s pr=%d comment=%d err=%v\n", j.RepoFullName, j.IssueNumber, j.PlaceholderID, editErr)
	}
}

// inlineReviewBody là body cấp-review (không phải body của từng comment) khi
// tạo review qua GitHub Reviews API — GitHub yêu cầu review phải có body
// hoặc ít nhất 1 comment; luôn có >=1 comment ở đây (postInlineComments chỉ
// được gọi khi len(inline) > 0) nên body chỉ mang tính chú thích, không bắt
// buộc phải có nội dung dài.
const inlineReviewBody = "Góp ý chi tiết theo từng dòng — xem tổng hợp đầy đủ ở comment phía trên."

// reviewCommentPayload là shape JSON GitHub Reviews API kỳ vọng cho 1 phần
// tử trong "comments" (xem GitHubClient.CreateReview). Side luôn "RIGHT" vì
// pendingComment.Line luôn là số dòng ở file MỚI (xem
// splitFindingsForPosting/FileDiff.LineAtNew) — finding gắn vào dòng bị xoá
// (chỉ tồn tại ở file cũ) không bao giờ tới được đây vì LineAtNew không
// khớp dòng removed.
//
// StartLine/StartSide chỉ gửi khi suggestion thay một khoảng nhiều dòng
// (pendingComment.StartLine > 0). GitHub bắt buộc start_side đi kèm
// start_line; bỏ trống với comment 1 dòng để omitempty không gửi 0.
type reviewCommentPayload struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	Line      int    `json:"line"`
	Side      string `json:"side"`
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
}

// postInlineComments gộp mọi pendingComment (từ mọi bundle, xem
// reviewBundles) thành 1 GitHub PR review duy nhất — thay vì mỗi bundle tự
// tạo 1 review riêng, gây rối cho người đọc khi PR bị chia nhiều bundle.
func (j *Job) postInlineComments(commitSHA string, inline []pendingComment) error {
	payload := make([]reviewCommentPayload, len(inline))
	for i, c := range inline {
		item := reviewCommentPayload{Path: c.Path, Line: c.Line, Side: "RIGHT", Body: c.Body}
		if c.StartLine > 0 && c.StartLine < c.Line {
			item.StartLine = c.StartLine
			item.StartSide = "RIGHT"
		}
		payload[i] = item
	}

	commentsJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cannot marshal inline comments: %w", err)
	}

	return j.GitHub.CreateReview(j.RepoFullName, j.IssueNumber, commitSHA, inlineReviewBody, commentsJSON)
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
//
// inline gộp lại pendingComment của MỌI bundle (xem splitFindingsForPosting,
// issue #5) — Run() post chúng thành 1 review duy nhất cho cả PR sau khi
// tất cả bundle chạy xong, thay vì mỗi bundle tự tạo 1 review riêng gây rối.
//
// validationDiff dùng để xác thực finding.File/Line — PHẢI là diff đầy đủ
// của cả PR (base...head, xem Run), KHÔNG phải bundleDiff của riêng bundle
// đang xử lý: bundleDiff có thể chỉ là phần thay đổi mới so với lần review
// trước (issue #21), có hunk khác với diff GitHub thực sự đối chiếu khi
// nhận inline comment.
//
// primer là ngữ cảnh dùng chung được build 1 lần cho cả PR (xem buildPrimer,
// issue #18) — nhúng y hệt vào MỌI bundle, rỗng khi PR không bị chia bundle
// (len(bundles) == 1, xem Run).
//
// allFindings gộp TOÀN BỘ finding parse được của MỌI bundle (kể cả những
// finding đã tách ra post inline riêng, khác `inline` ở trên vốn chỉ có
// pendingComment) — Run() dùng để render header tổng quan (renderReviewHeader)
// một lần cho cả comment, thay vì mỗi bundle tự có 1 mini-summary rời rạc.
//
// anyParsed báo có ÍT NHẤT 1 bundle parse JSON thành công hay không —
// header CHỈ nên hiện khi có dữ liệu có cấu trúc để tổng hợp; nếu Claude
// trả toàn văn xuôi tự do (bất chấp hướng dẫn) thì allFindings rỗng dù
// review không hề "sạch", hiện header "0 góp ý" lúc đó sẽ gây hiểu lầm —
// Run() dựa vào cờ này để giữ nguyên hành vi fallback raw-text cũ (không
// thêm header) thay vì suy diễn từ len(allFindings) == 0 (không phân biệt
// được "parse ra rỗng thật" với "chưa từng parse được").
//
// allParsed báo MỌI bundle đều parse JSON thành công (không bundle nào lỗi
// hay fallback raw text) — false nghĩa là allFindings không đại diện cho
// TOÀN BỘ PR: có phần nội dung (lỗi hoặc văn xuôi tự do) không được tính
// vào đó. Run() truyền partial vào renderReviewHeader: khi chưa đếm được
// finding nào thì header nói review chưa đủ để kết luận, không được mở đầu
// bằng "✅ không có vấn đề" (issue #69). Khi đã đếm được N thì cảnh báo
// đứng trước bảng, vì "Tổng: N" không phải toàn bộ PR (issue #57).
func (j *Job) reviewBundles(bundles []string, dir string, sha string, staticReport string, repoInstructions string, validationDiff string, primer string) (merged string, hadError bool, inline []pendingComment, allFindings []Finding, anyParsed bool, allParsed bool) {
	single := len(bundles) == 1
	sections := make([]string, len(bundles))
	parsedCount := 0

	for i, bundleDiff := range bundles {
		promptDiff := bundleDiff
		if !single && bundleDiff != "" {
			promptDiff = bundleNote(i+1, len(bundles)) + bundleDiff
		}

		prompt := BuildReviewPrompt(j.UserCommand, promptDiff, staticReport, repoInstructions, primer)
		cacheKey := bundleCacheKey(prompt)
		text, cached := j.loadCachedBundle(cacheKey)
		var err error
		errMsg := ""
		if cached {
			// Không gọi Reviewer nên không có gì để ghi reviewlog.
			fmt.Println("Review bundle", i+1, "/", len(bundles), "dùng kết quả đã lưu từ lần review bị ngắt trước")
		} else {
			start := time.Now()
			var stats CallStats
			text, stats, err = j.Reviewer.Review(prompt, dir)
			duration := time.Since(start)

			// Log response gốc của lần review (JSON nếu Claude làm đúng
			// format, text tự do nếu không — rỗng nếu lỗi), KHÔNG phải
			// display đã render lại. Ghi trước lần sửa định dạng bên dưới
			// để file log giữ đúng thứ tự: review trước, repair sau.
			if err != nil {
				errMsg = err.Error()
			}
			j.logBundleReview(sha, i+1, len(bundles), prompt, text, errMsg, duration, stats)
		}

		// display là những gì thực sự được post lên comment tổng hợp — mặc
		// định giống hệt text (raw), chỉ khác khi có lỗi (bọc thêm thông báo
		// lỗi) hoặc khi text parse được thành findings có cấu trúc (issue
		// #26): những finding gắn được vào đúng dòng diff thật tách ra
		// thành pendingComment (post riêng qua GitHub Reviews API, issue
		// #5), phần còn lại (general) mới render vào display.
		display := text
		if err != nil {
			fmt.Println("Review bundle", i+1, "/", len(bundles), "error:", err)
			display = "❌ Review thất bại: " + errMsg
			hadError = true
		} else {
			fmt.Println("Review bundle", i+1, "/", len(bundles), "result:", text)
			findings, ok := parseFindings(text)
			if !ok && strings.TrimSpace(text) == "" {
				// Output trắng không phải kết luận sạch. Ghi một câu để người
				// đọc thấy phần này không có kết quả, thay vì một mục trống.
				display = "⚠️ Reviewer không trả về nội dung cho phần này."
			} else if !ok {
				findings, ok = j.repairFindingsFormat(dir, sha, i+1, len(bundles), text)
			}
			if ok {
				anyParsed = true
				parsedCount++
				allFindings = append(allFindings, findings...)
				bundleInline, general := splitFindingsForPosting(validationDiff, findings)
				inline = append(inline, bundleInline...)
				display = renderBundleSummary(findings, general)
			}
			if !cached {
				j.saveCachedBundle(cacheKey, text, findings, ok)
			}
		}

		if single {
			sections[i] = display
		} else {
			sections[i] = fmt.Sprintf("### Phần %d/%d\n%s", i+1, len(bundles), display)
		}
	}

	allParsed = parsedCount == len(bundles)
	return strings.Join(sections, "\n\n"), hadError, inline, allFindings, anyParsed, allParsed
}

// bundleCacheKey là sha256 của prompt đầy đủ: prompt đã gồm diff của
// bundle, hướng dẫn repo, primer và lệnh của người review, nên mọi thay
// đổi đầu vào đều ra key khác, không dùng nhầm kết quả cũ.
func bundleCacheKey(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// loadCachedBundle đọc kết quả đã lưu của bundle. Cache lỗi chỉ log rồi coi
// như chưa có — resume là tối ưu, không được chặn review.
func (j *Job) loadCachedBundle(key string) (string, bool) {
	if j.BundleCache == nil {
		return "", false
	}
	text, found, err := j.BundleCache.LoadBundle(j.RepoFullName, j.IssueNumber, key)
	if err != nil {
		fmt.Println("Load bundle cache error (review lại bundle này):", err)
		return "", false
	}
	return text, found
}

// saveCachedBundle lưu kết quả của bundle vừa review xong. parsed=true thì
// lưu findings dạng JSON (kể cả khi phải qua lần sửa định dạng), để lần
// resume parse được ngay, không gọi lại repair. Không parse được thì lưu
// nguyên văn — lần review đó vẫn đã chạy xong. Output trắng không lưu:
// chạy lại có thể ra nội dung thật.
func (j *Job) saveCachedBundle(key, text string, findings []Finding, parsed bool) {
	if j.BundleCache == nil || (!parsed && strings.TrimSpace(text) == "") {
		return
	}
	if parsed {
		if findings == nil {
			findings = []Finding{} // nil marshal ra "null", parseFindings không đọc lại được
		}
		data, err := json.Marshal(findings)
		if err != nil {
			fmt.Println("Marshal findings for bundle cache error:", err)
			return
		}
		text = string(data)
	}
	if err := j.BundleCache.SaveBundle(j.RepoFullName, j.IssueNumber, key, text); err != nil {
		fmt.Println("Save bundle cache error:", err)
	}
}

// repairFindingsFormat gọi Reviewer thêm đúng 1 lần khi lần review đã chạy
// xong nhưng parseFindings thất bại (issue #69). Prompt chỉ mang output vừa
// rồi và yêu cầu JSON, không gửi lại diff. Lần này cũng thất bại thì caller
// giữ nguyên văn xuôi — không đặt hadError, vì review code đã chạy; lỗi sửa
// định dạng không được chặn ghi SHA nếu không review incremental sẽ lặp lại
// cùng diff mỗi lần model không tuân format.
//
// Khác retry trong claudecli.Reviewer: retry đó dành cho lỗi tạm thời của
// CLI (timeout, mạng). Ở đây CLI đã trả kết quả, chỉ sai format bên trong.
func (j *Job) repairFindingsFormat(dir, sha string, bundleIndex, bundleTotal int, previous string) ([]Finding, bool) {
	prompt := buildFormatRepairPrompt(previous)
	start := time.Now()
	text, stats, err := j.Reviewer.Review(prompt, dir)
	duration := time.Since(start)

	errMsg := ""
	if err != nil {
		fmt.Println("Repair bundle", bundleIndex, "/", bundleTotal, "format error:", err)
		errMsg = err.Error()
	}
	j.logBundleReview(sha, bundleIndex, bundleTotal, prompt, text, errMsg, duration, stats)
	if err != nil || strings.TrimSpace(text) == formatRepairNotAReview {
		return nil, false
	}
	findings, ok := parseFindings(text)
	// [] là một kết luận "sạch". Chỉ nhận khi văn xuôi lần trước thực sự
	// kết luận không có vấn đề, không phải một đoạn còn liệt kê bug.
	if !ok || (len(findings) == 0 && !proseConcludesClean(previous)) {
		return nil, false
	}
	return findings, true
}

// findingListPattern bắt danh sách đánh số hoặc tham chiếu file:dòng.
// Những đoạn đó không phải một câu kết luận sạch, dù có chứa cụm
// "không có vấn đề".
var findingListPattern = regexp.MustCompile(`(?:^|\n)\s*\d+[\.\)]\s|\S+\.\w+:\d+`)

// proseConcludesClean báo văn xuôi đã kết luận không có vấn đề đáng chú ý,
// nên lần sửa định dạng trả [] là hợp lệ.
func proseConcludesClean(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if findingListPattern.MatchString(lower) {
		return false
	}
	for _, phrase := range []string{
		"không có vấn đề",
		"không đáng chú ý",
		"no issues",
		"lgtm",
	} {
		from := 0
		for {
			i := strings.Index(lower[from:], phrase)
			if i < 0 {
				break
			}
			at := from + i
			if !cleanPhraseNegated(lower, at) {
				return true
			}
			from = at + len(phrase)
		}
	}
	return false
}

// cleanPhraseNegated báo cụm kết luận sạch đang bị phủ định ở ngay trước nó
// (vd "chưa thể kết luận là không có vấn đề").
func cleanPhraseNegated(lower string, at int) bool {
	start := at - 48
	if start < 0 {
		start = 0
	}
	window := lower[start:at]
	for _, neg := range []string{"chưa", "không phải", "not ", "never"} {
		if strings.Contains(window, neg) {
			return true
		}
	}
	return false
}

// logBundleReview ghi 1 lần gọi Reviewer. errMsg khác rỗng thì response ghi
// rỗng — lỗi CLI không có output đáng giữ, đúng như trước khi tách helper
// này ra (issue #9). Logger nil nghĩa là không ghi log.
func (j *Job) logBundleReview(sha string, bundleIndex, bundleTotal int, prompt, response, errMsg string, duration time.Duration, stats CallStats) {
	if j.Logger == nil {
		return
	}
	if errMsg != "" {
		response = ""
	}
	j.Logger.LogReview(j.RepoFullName, j.IssueNumber, sha, bundleIndex, bundleTotal, prompt, response, errMsg, duration, stats)
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
