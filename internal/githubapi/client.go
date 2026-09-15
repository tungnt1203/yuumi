package githubapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type CommentResponse struct {
	ID int64 `json:"id"`
}

type PullRequestResponse struct {
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
	// ChangedFiles là số file GitHub ghi nhận PR đã đổi — dùng để phát hiện
	// khi GetPullRequestDiff bị GitHub tự giới hạn/cắt bớt (xem
	// GetPullRequestChangedFilesCount).
	ChangedFiles int `json:"changed_files"`
}

// Client gọi GitHub REST API bằng 1 token cố định, thay vì phải truyền token
// vào từng lời gọi hàm như trước.
type Client struct {
	token string
}

func NewClient(token string) *Client {
	return &Client{token: token}
}

// newRequest tạo request kèm sẵn header Authorization + Accept dùng chung
// cho hầu hết endpoint (trừ GetPullRequestDiff, cần Accept khác).
func (c *Client) newRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
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

// getPullRequest gọi GET /repos/{repo}/pulls/{number} (JSON mặc định, không
// phải Accept diff của GetPullRequestDiff) — dùng chung cho
// GetPullRequestHeadSHA và GetPullRequestChangedFilesCount, vì cả 2 chỉ cần
// 2 field khác nhau từ CÙNG 1 response, không đáng gọi API 2 lần hay lặp
// lại boilerplate request/decode.
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
