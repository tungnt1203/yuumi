package review

import (
	"fmt"
	"strings"
)

// BuildReviewPrompt assembles the final prompt sent to the Claude CLI.
//
// userCommand is whatever the requester typed after mentioning the bot
// (e.g. "review", "review kỹ phần error handling"). diff is the real PR
// diff fetched from GitHub (see githubapi.GetPullRequestDiff) — it may be
// empty if fetching the diff failed, in which case Claude falls back to
// reading the checked-out file state only.
//
// The prompt explicitly tells Claude to read surrounding project files
// (README, related packages/conventions) before judging the diff, instead
// of reviewing the changed lines in isolation.
func BuildReviewPrompt(userCommand string, diff string) string {
	var b strings.Builder

	b.WriteString("Bạn đang review một Pull Request trong repo hiện tại (thư mục làm việc chính là repo đã checkout).\n\n")
	b.WriteString("Yêu cầu từ người review: ")
	b.WriteString(userCommand)
	b.WriteString("\n\n")

	if strings.TrimSpace(diff) != "" {
		b.WriteString("Đây là diff thật của PR (unified diff), review tập trung vào đúng các dòng thay đổi này:\n\n")
		b.WriteString("```diff\n")
		b.WriteString(diff)
		b.WriteString("\n```\n\n")
	} else {
		b.WriteString("Không lấy được diff thật của PR (có thể do lỗi gọi GitHub API). Hãy tự xác định phần thay đổi bằng cách đọc commit message và các file trong repo.\n\n")
	}

	b.WriteString("Trước khi kết luận, hãy đọc thêm các file liên quan trong repo (README, package/module xung quanh các file đã đổi) để hiểu đúng kiến trúc và convention của project — đừng chỉ nhìn diff một cách cô lập.\n")

	return b.String()
}

// bundleNote được chèn vào đầu diff khi PR quá lớn và bị chia thành nhiều
// bundle (xem bundleDiffs) — báo cho Claude biết nó chỉ đang thấy 1 phần
// của PR, để không kết luận nhầm (vd "PR chỉ sửa 3 file") khi thực ra còn
// các phần khác đang được review riêng.
func bundleNote(index, total int) string {
	return fmt.Sprintf(
		"[Lưu ý: PR này khá lớn nên được chia làm %d phần để review, đây là phần %d/%d. "+
			"Diff dưới đây KHÔNG phải toàn bộ PR — chỉ nhận xét dựa trên phần được giao, "+
			"đừng kết luận về những file không xuất hiện ở đây.]\n\n",
		total, index, total,
	)
}
