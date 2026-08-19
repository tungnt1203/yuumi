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
	"github.com/tungnt1203/yuumi/internal/review"
	"github.com/tungnt1203/yuumi/internal/webhook"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
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

		go func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Println("Recovered from panic:", r)
				}
			}()

			reviewText, err := claudecli.RunClaudeReview(cmd)
			if err != nil {
				fmt.Println("Claude error:", err)
				return
			}
			fmt.Println("Review result:", reviewText)

			err = githubapi.PostGitHubComment(payload.Repository.FullName, payload.Issue.Number, reviewText, cfg.GitHubToken)
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
