package review

import (
	"fmt"

	"github.com/tungnt1203/yuumi/internal/githubapi"
	"github.com/tungnt1203/yuumi/internal/gitrepo"
)

// Job đóng gói toàn bộ dữ liệu cần để thực hiện 1 lần review (clone repo,
// lấy diff, gọi Reviewer, sửa lại comment placeholder). Tách ra khỏi
// main.go để nơi nhận webhook (main.go) không cần biết chi tiết các bước
// bên trong — chỉ cần dựng Job rồi chạy go job.Run().
type Job struct {
	GitHub        *githubapi.Client
	Reviewer      Reviewer
	RepoFullName  string
	IssueNumber   int
	PlaceholderID int64
	UserCommand   string
}

// Run thực hiện review, nên luôn được gọi trong goroutine riêng
// (vd `go job.Run()`) vì có thể chạy lâu (clone repo, gọi Claude CLI).
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

	dir, cleanup, err := gitrepo.CloneRepo(j.RepoFullName, sha)
	if err != nil {
		fmt.Println("Clone repo error:", err)
		return
	}
	defer cleanup()

	diff, err := j.GitHub.GetPullRequestDiff(j.RepoFullName, j.IssueNumber)
	if err != nil {
		// Không chặn review nếu lấy diff lỗi — fallback về cách cũ
		// (Claude tự đọc file state + commit message).
		fmt.Println("Get pull request diff error:", err)
	}

	prompt := BuildReviewPrompt(j.UserCommand, diff)

	reviewText, err := j.Reviewer.Review(prompt, dir)
	if err != nil {
		if editErr := j.GitHub.EditComment(j.RepoFullName, j.PlaceholderID, "❌ Review thất bại: "+err.Error()); editErr != nil {
			fmt.Println("Edit comment error:", editErr)
		}
		return
	}

	fmt.Println("Review result:", reviewText)

	if err := j.GitHub.EditComment(j.RepoFullName, j.PlaceholderID, reviewText); err != nil {
		fmt.Println("Post comment error:", err)
		return
	}
	fmt.Println("Comment posted successfully")
}
