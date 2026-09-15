package review

import "strings"

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
