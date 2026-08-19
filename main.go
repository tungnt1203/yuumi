package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

type WebhookPayload struct {
	Action  string `json:"action"`
	Comment struct {
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
}

type Comment struct {
	Author string
	Body   string
}
type ClaudeResult struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

func (c Comment) MentionsBot(botName string) bool {
	return strings.Contains(c.Body, "@"+botName)
}
func (c Comment) ExtractCommand(botName string) (string, error) {
	if !c.MentionsBot(botName) {
		return "", fmt.Errorf("comment does not mention bot %s", botName)
	}

	body := c.Body
	mention := "@" + botName
	index := strings.Index(body, mention)
	return strings.TrimSpace(body[index+len(mention):]), nil
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

func RunClaudeReview(prompt string) (string, error) {
	cmd := exec.Command("claude", "-p", prompt, "--output-format", "json")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude command failed: %w", err)
	}

	var result ClaudeResult
	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("cannot parse claude output: %w", err)
	}

	if result.IsError {
		return "", fmt.Errorf("claude returned error: %s", result.Subtype)
	}

	return result.Result, nil
}
func main() {
	secret, ok := os.LookupEnv("GITHUB_WEBHOOK_SECRET")
	if !ok {
		log.Fatal("GITHUB_WEBHOOK_SECRET environment variable is required")
	}

	fmt.Println("Yuumi review bot starting...")
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "cannot read body", http.StatusBadRequest)
			return
		}

		signature := r.Header.Get("X-Hub-Signature-256")
		if !VerifySignature(secret, bodyBytes, signature) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		var payload WebhookPayload
		err = json.Unmarshal(bodyBytes, &payload)
		if err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		comment := Comment{
			Author: payload.Comment.User.Login,
			Body:   payload.Comment.Body,
		}

		cmd, err := comment.ExtractCommand("yuumi-bot")
		if err != nil {
			fmt.Println("Ignored:", err)
			fmt.Fprintln(w, "ignored")
			return
		}
		fmt.Println("Command from", comment.Author, ":", cmd)

		review, err := RunClaudeReview(cmd)
		if err != nil {
			fmt.Println("Claude error:", err)
			http.Error(w, "claude review failed", http.StatusInternalServerError)
			return
		}

		fmt.Println("Review result:", review)
		fmt.Fprintln(w, review)
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}
