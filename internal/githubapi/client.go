package githubapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
)

type CommentResponse struct {
	ID int64 `json:"id"`
}

type PullRequestResponse struct {
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
	// Base.SHA là commit của nhánh đích mà PR so sánh tới — dùng để đọc cấu
	// hình mà tác giả PR không tự sửa được (xem GetPullRequestSHAs).
	Base struct {
		SHA string `json:"sha"`
	} `json:"base"`
	// ChangedFiles là số file GitHub ghi nhận PR đã đổi — dùng để phát hiện
	// khi GetPullRequestDiff bị GitHub tự giới hạn/cắt bớt (xem
	// GetPullRequestChangedFilesCount).
	ChangedFiles int `json:"changed_files"`
}

// Client gọi GitHub REST API. Token được lấy lại cho TỪNG request qua
// token(), không giữ 1 chuỗi cố định: installation token của GitHub App chỉ
// sống 1 giờ, còn 1 job review có thể chờ trong hàng đợi Dispatcher rồi
// chạy nhiều bundle lâu hơn thế — token lấy lúc nhận webhook sẽ hết hạn
// trước khi job post kết quả (issue #99).
type Client struct {
	token func() (string, error)
}

// NewClient tạo Client dùng 1 token cố định (PAT, test, script ngắn).
func NewClient(token string) *Client {
	return NewClientWithTokenFunc(func() (string, error) { return token, nil })
}

// NewClientWithTokenFunc tạo Client gọi token() trước mỗi request. token
// nên có cache (vd githubapp.Provider) vì nó chạy mỗi lần gọi API.
func NewClientWithTokenFunc(token func() (string, error)) *Client {
	return &Client{token: token}
}

// newRequest tạo request kèm sẵn header Authorization + Accept dùng chung
// cho hầu hết endpoint (trừ GetPullRequestDiff, cần Accept khác).
func (c *Client) newRequest(method, url string, body io.Reader) (*http.Request, error) {
	token, err := c.token()
	if err != nil {
		return nil, fmt.Errorf("cannot get github token: %w", err)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	return req, nil
}

func (c *Client) PostComment(repoFullName string, issueNumber int, body string) (int64, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", repoFullName, issueNumber)
	reqBody, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return 0, fmt.Errorf("cannot marshal comment body: %w", err)
	}

	req, err := c.newRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("github api error %d: %s", resp.StatusCode, string(respBody))
	}

	var commentResponse CommentResponse
	if err := json.NewDecoder(resp.Body).Decode(&commentResponse); err != nil {
		return 0, fmt.Errorf("cannot decode comment response: %w", err)
	}

	return commentResponse.ID, nil
}

func (c *Client) AddReaction(repoFullName string, commentID int64) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/comments/%d/reactions", repoFullName, commentID)
	reqBody, err := json.Marshal(map[string]string{"content": "eyes"})
	if err != nil {
		return fmt.Errorf("cannot marshal reaction body: %w", err)
	}

	req, err := c.newRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *Client) EditComment(repoFullName string, commentID int64, body string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/comments/%d", repoFullName, commentID)
	reqBody, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("cannot marshal comment body: %w", err)
	}

	req, err := c.newRequest("PATCH", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// CreateReview tạo 1 PR review qua GitHub Reviews API
// (POST /pulls/{number}/reviews), post nhiều inline comment cùng lúc thay
// vì phải gọi riêng lẻ từng dòng — dùng để gắn góp ý review trực tiếp vào
// đúng dòng code thay đổi (xem review.Job.postInlineComments, issue #5).
//
// commentsJSON là mảng comment ĐÃ marshal sẵn (mỗi phần tử dạng
// {"path","line","side","body"}, thêm "start_line"/"start_side" khi comment
// phủ nhiều dòng) — Client không cần biết/định nghĩa struct
// gì về "comment" cả, chỉ nhúng thẳng vào body request qua json.RawMessage;
// caller (review.Job) chịu trách nhiệm đảm bảo đúng shape GitHub kỳ vọng.
//
// event luôn "COMMENT": bot chỉ góp ý, không tự ý APPROVE hay
// REQUEST_CHANGES — đó là quyết định của người review thật, không phải bot.
func (c *Client) CreateReview(repoFullName string, pullRequestNumber int, commitSHA string, body string, commentsJSON []byte) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d/reviews", repoFullName, pullRequestNumber)
	reqBody, err := json.Marshal(struct {
		CommitID string          `json:"commit_id"`
		Body     string          `json:"body,omitempty"`
		Event    string          `json:"event"`
		Comments json.RawMessage `json:"comments,omitempty"`
	}{
		CommitID: commitSHA,
		Body:     body,
		Event:    "COMMENT",
		Comments: commentsJSON,
	})
	if err != nil {
		return fmt.Errorf("cannot marshal review body: %w", err)
	}

	req, err := c.newRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// getPullRequest gọi GET /repos/{repo}/pulls/{number} (JSON mặc định, không
// phải Accept diff của GetPullRequestDiff) — dùng chung cho
// GetPullRequestHeadSHA, GetPullRequestSHAs và
// GetPullRequestChangedFilesCount, vì cả 3 chỉ cần vài field khác nhau từ
// CÙNG 1 response, không đáng lặp lại boilerplate request/decode.
func (c *Client) getPullRequest(repoFullName string, pullRequestNumber int) (PullRequestResponse, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repoFullName, pullRequestNumber)
	req, err := c.newRequest("GET", url, nil)
	if err != nil {
		return PullRequestResponse{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return PullRequestResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return PullRequestResponse{}, fmt.Errorf("github api error %d: %s", resp.StatusCode, string(respBody))
	}

	var pullRequestResponse PullRequestResponse
	if err := json.NewDecoder(resp.Body).Decode(&pullRequestResponse); err != nil {
		return PullRequestResponse{}, fmt.Errorf("cannot decode pull request response: %w", err)
	}

	return pullRequestResponse, nil
}

func (c *Client) GetPullRequestHeadSHA(repoFullName string, pullRequestNumber int) (string, error) {
	pr, err := c.getPullRequest(repoFullName, pullRequestNumber)
	if err != nil {
		return "", err
	}
	return pr.Head.SHA, nil
}

// GetPullRequestSHAs trả về SHA head và base của PR trong CÙNG 1 request.
// review.Job cần cả 2: head để review, base để đọc block_severity trong
// .yuumi.yml mà tác giả PR không tự sửa được (issue #60).
func (c *Client) GetPullRequestSHAs(repoFullName string, pullRequestNumber int) (headSHA string, baseSHA string, err error) {
	pr, err := c.getPullRequest(repoFullName, pullRequestNumber)
	if err != nil {
		return "", "", err
	}
	return pr.Head.SHA, pr.Base.SHA, nil
}

// GetFileContent đọc nội dung file path tại ref (SHA/nhánh) qua Contents
// API, dạng raw. File không tồn tại (404) trả found=false và err=nil — đa
// số repo không có file cấu hình, đó không phải lỗi.
func (c *Client) GetFileContent(repoFullName string, path string, ref string) (content []byte, found bool, err error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s?ref=%s", repoFullName, escapePath(path), neturl.QueryEscape(ref))
	req, err := c.newRequest("GET", url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github.raw+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("cannot read file response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("github api error %d: %s", resp.StatusCode, string(body))
	}
	return body, true, nil
}

// escapePath escape từng đoạn của path nhưng giữ nguyên "/" — PathEscape
// cho cả chuỗi sẽ biến "/" thành %2F, làm hỏng path lồng nhau (dir/file).
// Ký tự như "?", "#", "&" trong tên file không còn phá được cấu trúc URL.
func escapePath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = neturl.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

// GetPullRequestChangedFilesCount trả về số file GitHub ghi nhận PR đã đổi.
// review.Job dùng số này đối chiếu với số file thực sự parse được từ
// GetPullRequestDiff, để phát hiện trường hợp GitHub tự giới hạn/cắt bớt
// diff trả về (PR quá nhiều file) — khi đó review có thể sót file mà không
// ai biết nếu không đối chiếu.
func (c *Client) GetPullRequestChangedFilesCount(repoFullName string, pullRequestNumber int) (int, error) {
	pr, err := c.getPullRequest(repoFullName, pullRequestNumber)
	if err != nil {
		return 0, err
	}
	return pr.ChangedFiles, nil
}

// GetPullRequestDiff fetches the real unified diff of a PR from GitHub
// (via the "application/vnd.github.v3.diff" media type), so the reviewer
// knows exactly which lines changed instead of guessing from the checked-out
// file state. See gitrepo.CloneRepo's "--depth 1" limitation.
func (c *Client) GetPullRequestDiff(repoFullName string, pullRequestNumber int) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repoFullName, pullRequestNumber)
	req, err := c.newRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3.diff")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("cannot read diff response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("github api error %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// GetCompareDiff fetches the unified diff between 2 commits (base...head)
// via GitHub's compare API, cùng media type với GetPullRequestDiff — dùng
// để lấy CHỈ phần thay đổi MỚI khi 1 PR đã được review trước đó (xem
// review.Job.loadDiff, issue #21), thay vì lấy lại toàn bộ diff so với base
// mỗi lần review thêm.
func (c *Client) GetCompareDiff(repoFullName string, baseSHA string, headSHA string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/compare/%s...%s", repoFullName, baseSHA, headSHA)
	req, err := c.newRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3.diff")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("cannot read diff response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("github api error %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// CreateCheckRun tạo 1 check run ở trạng thái in_progress gắn với headSHA
// (POST /check-runs, issue #59) và trả về ID để CompleteCheckRun cập nhật
// khi review xong. Check run hiện trên tab Checks của PR và branch
// protection có thể bắt buộc nó pass.
func (c *Client) CreateCheckRun(repoFullName string, headSHA string, name string) (int64, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/check-runs", repoFullName)
	reqBody, err := json.Marshal(map[string]string{
		"name":     name,
		"head_sha": headSHA,
		"status":   "in_progress",
	})
	if err != nil {
		return 0, fmt.Errorf("cannot marshal check run body: %w", err)
	}

	req, err := c.newRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("github api error %d: %s", resp.StatusCode, string(respBody))
	}

	var checkRun struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&checkRun); err != nil {
		return 0, fmt.Errorf("cannot decode check run response: %w", err)
	}
	return checkRun.ID, nil
}

// CompleteCheckRun chuyển check run sang completed với conclusion
// (success/failure/neutral...), kèm title + summary (markdown) hiện ở tab
// Checks. detailsURL là link "Details" của check run — trỏ về comment
// review đầy đủ thay vì lặp lại toàn bộ nội dung trong summary.
func (c *Client) CompleteCheckRun(repoFullName string, checkRunID int64, conclusion string, title string, summary string, detailsURL string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/check-runs/%d", repoFullName, checkRunID)
	reqBody, err := json.Marshal(map[string]any{
		"status":      "completed",
		"conclusion":  conclusion,
		"details_url": detailsURL,
		"output": map[string]string{
			"title":   title,
			"summary": summary,
		},
	})
	if err != nil {
		return fmt.Errorf("cannot marshal check run body: %w", err)
	}

	req, err := c.newRequest("PATCH", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
