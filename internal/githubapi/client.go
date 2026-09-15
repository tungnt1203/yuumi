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

func (c *Client) GetPullRequestHeadSHA(repoFullName string, pullRequestNumber int) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repoFullName, pullRequestNumber)
	req, err := c.newRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github api error %d: %s", resp.StatusCode, string(respBody))
	}

	var pullRequestResponse PullRequestResponse
	if err := json.NewDecoder(resp.Body).Decode(&pullRequestResponse); err != nil {
		return "", fmt.Errorf("cannot decode pull request response: %w", err)
	}

	return pullRequestResponse.Head.SHA, nil
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
