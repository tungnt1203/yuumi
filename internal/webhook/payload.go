package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

type Payload struct {
	Action  string `json:"action"`
	Comment struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Issue struct {
		Number int `json:"number"`
	} `json:"issue"`

	// PullRequest chỉ có mặt trên webhook event "pull_request" (header
	// X-GitHub-Event, KHÔNG phải "action" — 2 event dùng chung tên field
	// "action" nhưng ý nghĩa khác nhau), dùng để tự động review khi PR mới
	// mở hoặc có commit mới push lên, không chỉ khi được mention (issue
	// #32). Ở event "issue_comment" field này giữ nguyên zero value, không
	// ảnh hưởng gì tới luồng mention hiện có.
	PullRequest struct {
		Number int `json:"number"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`

	// Installation là ID của lần cài GitHub App vào 1 repo/org — GitHub tự
	// đính kèm field này vào MỌI webhook payload khi App đã được cài (issue
	// #47), dùng để đổi lấy đúng installation access token cho repo gửi
	// webhook (xem githubapp.Provider.Token). Không có mặt nếu bot còn xác
	// thực bằng PAT (không áp dụng cho webhook thật vì App bắt buộc theo
	// issue #47).
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// PullRequestAutoReviewActions liệt kê action của event "pull_request" nên
// kích hoạt auto-review (issue #32): "opened" (PR mới tạo) và "synchronize"
// (có commit mới push lên PR) — các action khác (closed, reopened, edited,
// labeled, review_requested...) không phải "có code mới cần review" nên
// không kích hoạt gì cả.
var PullRequestAutoReviewActions = map[string]bool{
	"opened":      true,
	"synchronize": true,
}

func VerifySignature(secret string, payload []byte, signatureHeader string) bool {
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return false
	}

	expectedHex := strings.TrimPrefix(signatureHeader, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	computedHex := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(computedHex), []byte(expectedHex))
}
