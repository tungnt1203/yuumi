package githubapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func PostGitHubComment(repoFullName string, issueNumber int, body string, token string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", repoFullName, issueNumber)
	reqBody, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("cannot marshal comment body: %w", err)
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
