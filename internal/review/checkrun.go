package review

import "fmt"

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

// reviewedCheckRunResult dựng kết quả check run khi review đã post xong.
//
// Chưa có severity gate (#60) nên review xong trọn vẹn luôn là success, dù
// có finding gì — giống hành vi hiện tại chỉ comment, không chặn merge.
// hadError (có bundle lỗi) là neutral: review chưa đủ để kết luận.
//
// header là bảng tổng hợp của comment (renderReviewHeader), rỗng khi không
// có finding nào được cấu trúc (Claude trả văn xuôi, hoặc không có file nào
// cần review) — khi đó summary chỉ trỏ về comment.
//
// partial: có phần review parse được, có phần là văn xuôi (giống cảnh báo
// "còn phần chưa đếm" của header) — title phải nói rõ số góp ý chưa đủ.
func reviewedCheckRunResult(hadError bool, parsed bool, partial bool, findingCount int, header string, commentURL string) checkRunResult {
	result := checkRunResult{Conclusion: "success"}
	switch {
	case hadError:
		result.Conclusion = "neutral"
		result.Title = "Review chưa trọn vẹn: một phần bị lỗi"
	case !parsed:
		result.Title = "Review xong"
	case partial:
		result.Title = fmt.Sprintf("%d góp ý, còn phần chưa đếm", findingCount)
	case findingCount == 0:
		result.Title = "Không phát hiện vấn đề"
	default:
		result.Title = fmt.Sprintf("%d góp ý", findingCount)
	}

	summary := header
	if summary == "" {
		summary = "Xem nội dung review trong comment trên PR."
	}
	result.Summary = fmt.Sprintf("%s\n\n[Xem review đầy đủ trên PR](%s)", summary, commentURL)
	return result
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
