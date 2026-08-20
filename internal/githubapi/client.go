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
