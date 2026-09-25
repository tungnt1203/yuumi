package review

import (
	"fmt"
	"slices"
	"strings"
)

// checkRunName là tên check run hiện trên tab Checks của PR — cũng là tên
// phải chọn khi cấu hình branch protection bắt buộc review pass (issue #59).
const checkRunName = "yuumi review"

// checkRunResult là kết quả ghi lên check run khi review kết thúc.
type checkRunResult struct {
	Conclusion string
	Title      string
	Summary    string
}

// failedCheckRunResult là kết quả mặc định: Run khởi tạo check run với giá
// trị này rồi chỉ đổi sang kết quả thật khi review post xong, nên mọi đường
// thoát giữa chừng (clone lỗi, post comment lỗi, panic) đều kết thúc check
// run bằng neutral thay vì để treo ở in_progress.
var failedCheckRunResult = checkRunResult{
	Conclusion: "neutral",
	Title:      "Review thất bại",
	Summary:    "Review không chạy xong (lỗi clone, gọi Claude CLI hoặc post kết quả). Xem log server để biết chi tiết.",
}

// reviewOutcome là những gì reviewedCheckRunResult cần biết về 1 lần review
// đã post xong.
type reviewOutcome struct {
	HadError bool // có bundle lỗi
	Parsed   bool // có ít nhất 1 bundle trả finding dạng cấu trúc
	Partial  bool // có bundle parse được, có bundle là văn xuôi
	Findings []Finding

	// Header là bảng tổng hợp của comment (renderReviewHeader), rỗng khi
	// không có finding nào được cấu trúc — khi đó summary chỉ trỏ về comment.
	Header     string
	CommentURL string

	// BlockSeverity là các mức làm check run failure (severity gate, issue
	// #60), đã chuẩn hoá chữ thường. GateNote là cảnh báo khi không đọc được
	// cấu hình gate (gate khi đó không áp dụng).
	BlockSeverity []string
	GateNote      string

	// UnreviewedSubmodules là số submodule PR đưa code mới vào mà bot không
	// review (issue #93).
	UnreviewedSubmodules int
}

// reviewedCheckRunResult dựng kết quả check run khi review đã post xong.
//
//   - Có finding ở mức trong BlockSeverity: failure, kể cả khi có bundle lỗi
//     — vấn đề đã tìm thấy là thật, phần lỗi chỉ có thể thêm vấn đề.
//   - Có bundle lỗi: neutral, review chưa đủ để kết luận.
//   - Có submodule mang code mới chưa review: neutral, cùng lý do.
//   - Còn lại: success. BlockSeverity rỗng (mặc định) thì luôn thế, dù có
//     finding gì — giữ hành vi chỉ góp ý, không chặn merge.
func reviewedCheckRunResult(o reviewOutcome) checkRunResult {
	blocked := blockedFindingCount(o.Findings, o.BlockSeverity)

	result := checkRunResult{Conclusion: "success"}
	switch {
	case blocked > 0:
		result.Conclusion = "failure"
		result.Title = fmt.Sprintf("%d góp ý ở mức chặn merge (%s)", blocked, strings.ToUpper(strings.Join(o.BlockSeverity, "/")))
	case o.HadError:
		result.Conclusion = "neutral"
		result.Title = "Review chưa trọn vẹn: một phần bị lỗi"
	case o.UnreviewedSubmodules > 0:
		result.Conclusion = "neutral"
		result.Title = fmt.Sprintf("%d submodule chưa được review", o.UnreviewedSubmodules)
		if len(o.Findings) > 0 {
			result.Title = fmt.Sprintf("%d góp ý; %d submodule chưa được review", len(o.Findings), o.UnreviewedSubmodules)
		}
	case !o.Parsed:
		result.Title = "Review xong"
	case o.Partial:
		result.Title = fmt.Sprintf("%d góp ý, còn phần chưa đếm", len(o.Findings))
	case len(o.Findings) == 0:
		result.Title = "Không phát hiện vấn đề"
	default:
		result.Title = fmt.Sprintf("%d góp ý", len(o.Findings))
	}

	// Gate có thể vừa bật (các giá trị hợp lệ) vừa có cảnh báo (giá trị gõ
	// sai) — hiện cả 2.
	var parts []string
	if o.GateNote != "" {
		parts = append(parts, o.GateNote)
	}
	if len(o.BlockSeverity) > 0 {
		parts = append(parts, fmt.Sprintf("Severity gate: finding mức **%s** làm check này fail (`block_severity` trong `.yuumi.yml` của nhánh base).", strings.ToUpper(strings.Join(o.BlockSeverity, ", "))))
	}
	if o.Header != "" {
		parts = append(parts, o.Header)
	} else {
		parts = append(parts, "Xem nội dung review trong comment trên PR.")
	}
	parts = append(parts, fmt.Sprintf("[Xem review đầy đủ trên PR](%s)", o.CommentURL))
	result.Summary = strings.Join(parts, "\n\n")
	return result
}

// blockedFindingCount đếm finding có severity nằm trong block.
func blockedFindingCount(findings []Finding, block []string) int {
	if len(block) == 0 {
		return 0
	}
	count := 0
	for _, f := range findings {
		if slices.Contains(block, strings.ToLower(strings.TrimSpace(f.Severity))) {
			count++
		}
	}
	return count
}

// loadBlockSeverity đọc block_severity từ .yuumi.yml ở commit BASE của PR
// qua GitHub API, không phải bản trong thư mục clone (head): tác giả PR sửa
// được .yuumi.yml ở head, nên đọc từ đó thì chính PR cần bị chặn tự tắt
// được gate. Muốn đổi gate phải merge thay đổi vào base trước.
//
// Không có file / không set: trả nil, gate tắt. Lỗi (API, YAML sai): trả nil
// kèm note để hiện trên check run — gate không áp dụng thay vì làm fail
// mọi PR chỉ vì GitHub API chập chờn.
//
// Giá trị gõ sai (vd "critcal") bị bỏ qua, và note nêu rõ trên check run —
// nếu chỉ log, người cấu hình tưởng gate đang chặn mức đó trong khi không.
func (j *Job) loadBlockSeverity(baseSHA string) (block []string, note string) {
	const failNote = "⚠️ Không đọc được `block_severity` trong `.yuumi.yml` của nhánh base, severity gate không áp dụng cho lần review này. Xem log server."

	if baseSHA == "" {
		fmt.Println("Severity gate: PR không có base SHA")
		return nil, failNote
	}
	data, found, err := j.GitHub.GetFileContent(j.RepoFullName, repoConfigFileName, baseSHA)
	if err != nil {
		fmt.Println("Get base .yuumi.yml error:", err)
		return nil, failNote
	}
	if !found {
		return nil, ""
	}
	cfg, err := parseRepoConfig(data)
	if err != nil {
		fmt.Println("Parse base .yuumi.yml error:", err)
		return nil, failNote
	}

	block, invalid := normalizeSeverities(cfg.BlockSeverity)
	if len(invalid) > 0 {
		fmt.Println("WARNING: block_severity có giá trị không hợp lệ (bỏ qua):", invalid)
		return block, fmt.Sprintf("⚠️ `block_severity` trong `.yuumi.yml` của nhánh base có giá trị không hợp lệ, đã bỏ qua: `%s`. Giá trị hợp lệ: critical, high, medium, low.", strings.Join(invalid, "`, `"))
	}
	return block, ""
}

// startCheckRun tạo check run in_progress cho sha. Lỗi chỉ log và trả 0 —
// check run là phần phụ, không được chặn review (vd App chưa được cấp
// quyền checks).
func (j *Job) startCheckRun(sha string) int64 {
	id, err := j.GitHub.CreateCheckRun(j.RepoFullName, sha, checkRunName)
	if err != nil {
		fmt.Println("Create check run error (bỏ qua):", err)
		return 0
	}
	return id
}

// finishCheckRun chuyển check run sang completed. id == 0 (tạo lỗi ở
// startCheckRun) thì bỏ qua.
func (j *Job) finishCheckRun(id int64, result checkRunResult) {
	if id == 0 {
		return
	}
	if err := j.GitHub.CompleteCheckRun(j.RepoFullName, id, result.Conclusion, result.Title, result.Summary, j.reviewCommentURL()); err != nil {
		fmt.Println("Complete check run error:", err)
	}
}

// reviewCommentURL là link tới comment review (placeholder đã được sửa
// thành kết quả) — dùng làm "Details" của check run.
func (j *Job) reviewCommentURL() string {
	return fmt.Sprintf("https://github.com/%s/pull/%d#issuecomment-%d", j.RepoFullName, j.IssueNumber, j.PlaceholderID)
}
