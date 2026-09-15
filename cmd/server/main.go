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
	"github.com/tungnt1203/yuumi/internal/healthcheck"
	"github.com/tungnt1203/yuumi/internal/review"
	"github.com/tungnt1203/yuumi/internal/reviewlog"
	"github.com/tungnt1203/yuumi/internal/reviewstate"
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
	dispatcher := review.NewDispatcher(cfg.MaxConcurrentReviews)
	seenComments := webhook.NewSeenComments()
	reviewLogger := reviewlog.NewFileLogger(cfg.ReviewLogDir)
	reviewStateStore := reviewstate.NewFileStore(cfg.ReviewStateFile)

	// Check claude CLI + GITHUB_TOKEN thật sự dùng được ngay lúc khởi động,
	// thay vì chỉ tin biến môi trường đã set là đủ — nếu không, lỗi (CLI
	// chưa authenticate, token hết hạn...) chỉ lộ ra khi có webhook thật
	// tới (xem issue #29). Không Fatal ở đây: tránh crash loop nếu chỉ là
	// sự cố mạng thoáng qua lúc deploy, nhưng phải log đủ rõ để không bị
	// bỏ sót.
	healthMonitor := healthcheck.NewMonitor(cfg.GitHubToken)
	if report := healthMonitor.Check(); !report.Healthy() {
		if !report.ClaudeCLI.OK {
			log.Println("WARNING: claude CLI check thất bại:", report.ClaudeCLI.Message)
		}
		if !report.GitHubToken.OK {
			log.Println("WARNING: GITHUB_TOKEN check thất bại:", report.GitHubToken.Message)
		}
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		report := healthMonitor.Last()
		w.Header().Set("Content-Type", "application/json")
		if !report.Healthy() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(report)
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

		if !seenComments.MarkIfNew(payload.Comment.ID) {
			fmt.Println("Ignored: duplicate comment ID", payload.Comment.ID)
			fmt.Fprintln(w, "ignored")
			return
		}

		if err := ghClient.AddReaction(payload.Repository.FullName, payload.Comment.ID); err != nil {
			fmt.Println("Add reaction error:", err)
		}

		placeholderID, err := ghClient.PostComment(payload.Repository.FullName, payload.Issue.Number, "Đang review...")
		if err != nil {
			fmt.Println("Post comment error:", err)
			return
		}

		job := &review.Job{
			GitHub:            ghClient,
			Clone:             gitrepo.CloneRepo,
			Reviewer:          reviewer,
			RepoFullName:      payload.Repository.FullName,
			IssueNumber:       payload.Issue.Number,
			PlaceholderID:     placeholderID,
			UserCommand:       cmd,
			BundleBudgetChars: cfg.MaxDiffBundleChars,
			Logger:            reviewLogger,
			StateStore:        reviewStateStore,
		}
		dispatcher.Submit(job.Run)

		fmt.Fprintln(w, "processing")
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
