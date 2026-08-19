package claudecli

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"
    "time"
	"bytes"
)

type ClaudeResult struct {
    Type    string `json:"type"`
    Subtype string `json:"subtype"`
    IsError bool   `json:"is_error"`
    Result  string `json:"result"`
}

func RunClaudeReview(prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "-p", prompt, "--output-format", "json")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude command failed: %w (stderr: %s)", err, stderr.String())
	}

	var result ClaudeResult
	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("cannot parse claude output: %w", err)
	}

	if result.IsError {
		return "", fmt.Errorf("claude returned error (%s): %s", result.Subtype, result.Result)
	}

	return result.Result, nil
}
