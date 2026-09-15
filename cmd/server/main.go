package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"slices"

	"github.com/tungnt1203/yuumi/internal/claudecli"
	"github.com/tungnt1203/yuumi/internal/config"
	"github.com/tungnt1203/yuumi/internal/githubapi"
	"github.com/tungnt1203/yuumi/internal/gitrepo"
	"github.com/tungnt1203/yuumi/internal/review"
	"github.com/tungnt1203/yuumi/internal/webhook"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Yuumi review bot starting...")

	ghClient := githubapi.NewClient(cfg.GitHubToken)
	var reviewer review.Reviewer = claudecli.NewReviewer()

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
		if !webhook.VerifySignature(cfg.WebhookSecret, bodyBytes, signature) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		var payload webhook.Payload
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if payload.Action != "created" {
			fmt.Println("Ignored: action is", payload.Action)
			fmt.Fprintln(w, "ignored")
			return
		}

		comment := review.Comment{
			Author: payload.Comment.User.Login,
			Body:   payload.Comment.Body,
		}

		if !slices.Contains(cfg.AllowedUsers, comment.Author) {
			fmt.Println("Rejected: unauthorized user", comment.Author)
			http.Error(w, "unauthorized", http.StatusForbidden)
			return
		}

		cmd, err := comment.ExtractCommand("yuumi-bot")
		if err != nil {
			fmt.Println("Ignored:", err)
			fmt.Fprintln(w, "ignored")
			return
		}
		fmt.Println("Command from", comment.Author, ":", cmd)
		fmt.Println("Repo:", payload.Repository.FullName, "| Issue #:", payload.Issue.Number)

		if err := ghClient.AddReaction(payload.Repository.FullName, payload.Comment.ID); err != nil {
			fmt.Println("Add reaction error:", err)
		}

		placeholderID, err := ghClient.PostComment(payload.Repository.FullName, payload.Issue.Number, "Đang review...")
		if err != nil {
			fmt.Println("Post comment error:", err)
			return
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Println("Recovered from panic:", r)
				}
			}()

			sha, err := ghClient.GetPullRequestHeadSHA(payload.Repository.FullName, payload.Issue.Number)

			if err != nil {
				fmt.Println("Get pull request head SHA error:", err)
				return
			}

			dir, cleanup, err := gitrepo.CloneRepo(payload.Repository.FullName, sha)
			if err != nil {
				fmt.Println("Clone repo error:", err)
				return
			}
			defer cleanup()

			diff, err := ghClient.GetPullRequestDiff(payload.Repository.FullName, payload.Issue.Number)
			if err != nil {
				// Không chặn review nếu lấy diff lỗi — fallback về cách cũ
				// (Claude tự đọc file state + commit message).
				fmt.Println("Get pull request diff error:", err)
			}

			prompt := review.BuildReviewPrompt(cmd, diff)

			reviewText, err := reviewer.Review(prompt, dir)
			if err != nil {
				if editErr := ghClient.EditComment(payload.Repository.FullName, placeholderID, "❌ Review thất bại: "+err.Error()); editErr != nil {
					fmt.Println("Edit comment error:", editErr)
				}
				return
			}

			fmt.Println("Review result:", reviewText)

			err = ghClient.EditComment(payload.Repository.FullName, placeholderID, reviewText)
			if err != nil {
				fmt.Println("Post comment error:", err)
				return
			}
			fmt.Println("Comment posted successfully")
		}()

		fmt.Fprintln(w, "processing")
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
