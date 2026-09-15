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

func PostGitHubComment(repoFullName string, issueNumber int, body string, token string) (int64, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", repoFullName, issueNumber)
	reqBody, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return 0, fmt.Errorf("cannot marshal comment body: %w", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return 0, fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
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

func AddReaction(repoFullName string, commentID int64, token string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/comments/%d/reactions", repoFullName, commentID)
	reqBody, err := json.Marshal(map[string]string{"content": "eyes"})
	if err != nil {
		return fmt.Errorf("cannot marshal reaction body: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
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

func EditGitHubComment(repoFullName string, commentID int64, body string, token string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/comments/%d", repoFullName, commentID)
	reqBody, err := json.Marshal(map[string]string{"body": body})

	if err != nil {
		return fmt.Errorf("cannot marshal comment body: %w", err)
	}

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
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

func GetPullRequestHeadSHA(repoFullName string, pullRequestNumber int, token string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repoFullName, pullRequestNumber)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

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
func GetPullRequestDiff(repoFullName string, pullRequestNumber int, token string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repoFullName, pullRequestNumber)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("cannot create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
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
