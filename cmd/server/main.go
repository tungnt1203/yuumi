package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/tungnt1203/yuumi/internal/claudecli"
	"github.com/tungnt1203/yuumi/internal/config"
	"github.com/tungnt1203/yuumi/internal/egress"
	"github.com/tungnt1203/yuumi/internal/githubapi"
	"github.com/tungnt1203/yuumi/internal/githubapp"
	"github.com/tungnt1203/yuumi/internal/gitrepo"
	"github.com/tungnt1203/yuumi/internal/healthcheck"
	"github.com/tungnt1203/yuumi/internal/review"
	"github.com/tungnt1203/yuumi/internal/reviewlog"
	"github.com/tungnt1203/yuumi/internal/reviewstate"
	"github.com/tungnt1203/yuumi/internal/sandbox"
	"github.com/tungnt1203/yuumi/internal/webhook"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Yuumi review bot starting...")

	// tokenProvider giữ App ID + private key cố định suốt vòng đời server,
	// tự ký JWT/xin installation token và cache theo installationID (xem
	// package githubapp, issue #47) — KHÔNG còn 1 ghClient dùng chung, vì
	// token giờ gắn theo installation của từng webhook (xem newJob).
	tokenProvider := githubapp.NewProvider(cfg.GitHubAppID, cfg.GitHubAppPrivateKey)
	var reviewer review.Reviewer = &claudecli.Reviewer{Timeout: time.Duration(cfg.ReviewTimeoutMinutes) * time.Minute}
	dispatcher := review.NewDispatcher(cfg.MaxConcurrentReviews)
	seenComments := webhook.NewSeenComments()
	reviewLogger := reviewlog.NewFileLogger(cfg.ReviewLogDir)
	reviewStateStore := reviewstate.NewFileStore(cfg.ReviewStateFile)
	bundleCache := reviewstate.NewBundleCache(cfg.BundleCacheDir)

	// Check claude CLI + GitHub App auth thật sự dùng được ngay lúc khởi
	// động, thay vì chỉ tin biến môi trường đã set là đủ — nếu không, lỗi
	// (CLI chưa authenticate, App ID/private key sai...) chỉ lộ ra khi có
	// webhook thật tới (xem issue #29). Không Fatal ở đây: tránh crash loop
	// nếu chỉ là sự cố mạng thoáng qua lúc deploy, nhưng phải log đủ rõ để
	// không bị bỏ sót.
	healthMonitor := healthcheck.NewMonitor(cfg.GitHubAppID, cfg.GitHubAppPrivateKey)
	logUnhealthy := func(report healthcheck.Report) {
		if !report.ClaudeCLI.OK {
			log.Println("WARNING: claude CLI check thất bại:", report.ClaudeCLI.Message)
		}
		if !report.GitHubApp.OK {
			log.Println("WARNING: GitHub App auth check thất bại:", report.GitHubApp.Message)
		}
	}
	logUnhealthy(healthMonitor.Check())
	// Check lại định kỳ để /health (và Docker HEALTHCHECK) không kẹt ở kết
	// quả lúc khởi động. 5 phút: đủ nhanh để phát hiện token hỏng, mà chỉ
	// tốn ~12 lần gọi GitHub API/giờ. Server chưa có graceful shutdown nên
	// dùng context.Background().
	go healthMonitor.Run(context.Background(), 5*time.Minute, logUnhealthy)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		report := healthMonitor.Last()
		w.Header().Set("Content-Type", "application/json")
		if !report.Healthy() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(report)
	})

	// newJob dựng 1 review.Job dùng chung cấu hình (cloner, reviewer, budget,
	// logger, state store) cho CẢ 2 luồng trigger (mention thủ công lẫn
	// auto-review, issue #32) — chỉ khác nhau ở ghClient (token riêng theo
	// installation của từng webhook, xem githubapp.Provider, issue #47) và
	// RepoFullName/IssueNumber/PlaceholderID/UserCommand, tránh 2 luồng tự
	// xây dựng Job lệch nhau.
	//
	// token trả installation token còn hạn — clone gọi nó ngay lúc clone
	// (có thể sau khi job chờ lâu trong hàng đợi Dispatcher) để đọc được
	// repo private (issue #48, #99).
	// startSandbox: nil giữ hành vi cũ (lệnh trên code PR chạy thẳng trên
	// server); SANDBOX=docker cho mỗi job 1 container riêng (issue #78).
	var startSandbox func(dir string) (sandbox.Env, error)
	if cfg.Sandbox == "docker" {
		// Sandbox chỉ nhận credential Claude GIẢ cùng loại; credential thật
		// nằm ở credential proxy trong container egress (issue #78 bước 3).
		cred, err := egress.CredentialFromOSEnv()
		if err != nil {
			log.Fatal("SANDBOX=docker: ", err)
		}
		dockerCfg := sandbox.DockerConfig{Image: cfg.SandboxImage, CredentialEnv: cred.Env}
		startSandbox = func(dir string) (sandbox.Env, error) {
			return sandbox.StartDocker(dockerCfg, dir)
		}
		// Network --internal + egress proxy (issue #78 bước 2). Không dựng
		// được thì dừng: chạy sandbox mà không giới hạn mạng là âm thầm mất
		// lớp bảo vệ đã bật.
		if err := sandbox.SetupNetwork(cfg.SandboxImage); err != nil {
			log.Fatal("cannot set up sandbox network: ", err)
		}
		fmt.Println("Sandbox: docker, image", cfg.SandboxImage, "| work dir", cfg.WorkDir, "| network", sandbox.NetworkName)
	}
	if cfg.WorkDir != "" {
		if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
			log.Fatal("cannot create WORK_DIR: ", err)
		}
	}

	// installationToken trả hàm lấy installation token còn hạn cho 1
	// installation. Client và clone gọi nó mỗi lần cần, không giữ token lấy
	// lúc nhận webhook: token chỉ sống 1 giờ, job có thể chờ hàng đợi rồi
	// chạy lâu hơn thế (issue #99). Provider cache và tự làm mới trước khi
	// hết hạn, nên gọi nhiều lần không tốn request tới GitHub. Dùng
	// context.Background: job chạy sau khi HTTP request của webhook đã xong.
	installationToken := func(installationID int64) func() (string, error) {
		return func() (string, error) {
			return tokenProvider.Token(context.Background(), installationID)
		}
	}

	newJob := func(ghClient *githubapi.Client, token func() (string, error), repoFullName string, issueNumber int, placeholderID int64, userCommand string) *review.Job {
		return &review.Job{
			GitHub: ghClient,
			Clone: func(repoFullName, sha string) (string, func(), error) {
				tok, err := token()
				if err != nil {
					return "", nil, fmt.Errorf("get installation token for clone: %w", err)
				}
				return gitrepo.CloneRepo(repoFullName, sha, tok, cfg.WorkDir)
			},
			StartSandbox:      startSandbox,
			Reviewer:          reviewer,
			RepoFullName:      repoFullName,
			IssueNumber:       issueNumber,
			PlaceholderID:     placeholderID,
			UserCommand:       userCommand,
			BundleBudgetChars: cfg.MaxDiffBundleChars,
			Logger:            reviewLogger,
			StateStore:        reviewStateStore,
			BundleCache:       bundleCache,
		}
	}

	// handleIssueComment xử lý luồng review theo mention thủ công
	// ("@yuumi review" trong comment PR) — hành vi giữ nguyên như trước
	// issue #32, chỉ tách ra khỏi handler chính để handler chính route được
	// theo loại event (xem handlePullRequest cho luồng auto-review mới).
	handleIssueComment := func(w http.ResponseWriter, r *http.Request, payload webhook.Payload) {
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

		cmd, err := comment.ExtractCommand("yuumi")
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
		// Đánh dấu trước để hai webhook cùng comment không tạo hai job.
		// Nếu thoát vì lỗi trước khi post placeholder, Forget để GitHub
		// redeliver (response 5xx) được xử lý lại. SHA đã review thì giữ
		// dấu: gửi lại comment đó không có việc mới.
		keepSeen := false
		defer func() {
			if !keepSeen {
				seenComments.Forget(payload.Comment.ID)
			}
		}()

		// Xin installation token đúng lúc này (không sớm hơn): mọi check ở
		// trên đều rẻ và không cần gọi GitHub, để request bị ignore/reject
		// (sai user, comment cũ, action khác "created"...) không tốn thêm 1
		// lần gọi mạng đổi token vô ích (xem githubapp.Provider, issue #47).
		token := installationToken(payload.Installation.ID)
		if _, err := token(); err != nil {
			fmt.Println("Get installation token error:", err)
			http.Error(w, "cannot authenticate with github", http.StatusInternalServerError)
			return
		}
		ghClient := githubapi.NewClientWithTokenFunc(token)

		// PR đã được review xong tới đúng head SHA hiện tại (vd auto-review
		// đã chạy lúc mở/push, giờ có người mention lại) — không có gì mới,
		// tốn thêm 1 lần gọi Claude CLI chỉ để nhận ra vậy là phí (issue
		// #32, dùng chung AlreadyReviewedSHA với luồng auto-review bên
		// dưới). Lỗi hoặc chưa review lần nào đều review bình thường.
		if headSHA, err := ghClient.GetPullRequestHeadSHA(payload.Repository.FullName, payload.Issue.Number); err == nil {
			if review.AlreadyReviewedSHA(reviewStateStore, payload.Repository.FullName, payload.Issue.Number, headSHA) {
				fmt.Println("Ignored: PR already reviewed at SHA", headSHA)
				keepSeen = true
				fmt.Fprintln(w, "ignored")
				return
			}
		}

		if err := ghClient.AddReaction(payload.Repository.FullName, payload.Comment.ID); err != nil {
			fmt.Println("Add reaction error:", err)
		}

		placeholderID, err := ghClient.PostComment(payload.Repository.FullName, payload.Issue.Number, "Đang review...")
		if err != nil {
			fmt.Println("Post comment error:", err)
			http.Error(w, "cannot post placeholder", http.StatusInternalServerError)
			return
		}

		keepSeen = true
		dispatcher.Submit(newJob(ghClient, token, payload.Repository.FullName, payload.Issue.Number, placeholderID, cmd).Run)

		fmt.Fprintln(w, "processing")
	}

	// handlePullRequest xử lý luồng auto-review khi PR mới mở hoặc có
	// commit mới push lên (issue #32), không cần ai mention bot.
	//
	// Allowlist: ALLOWED_USERS (vốn dùng để chặn ai được PHÉP mention bot ở
	// luồng issue_comment) được tái dùng ở đây để quyết định auto-review áp
	// dụng cho PR của AI — chỉ auto-review PR do chính người trong danh
	// sách này tạo, tránh review "miễn phí" mọi PR của bất kỳ ai gửi vào
	// repo đã cài webhook (xem README mục "Auto review").
	handlePullRequest := func(w http.ResponseWriter, r *http.Request, payload webhook.Payload) {
		if !webhook.PullRequestAutoReviewActions[payload.Action] {
			fmt.Println("Ignored: pull_request action is", payload.Action)
			fmt.Fprintln(w, "ignored")
			return
		}

		author := payload.PullRequest.User.Login
		if !slices.Contains(cfg.AllowedUsers, author) {
			fmt.Println("Ignored: auto-review skipped, PR author not in ALLOWED_USERS:", author)
			fmt.Fprintln(w, "ignored")
			return
		}

		repoFullName := payload.Repository.FullName
		issueNumber := payload.PullRequest.Number
		headSHA := payload.PullRequest.Head.SHA
		fmt.Println("Auto-review trigger from", author, "| Repo:", repoFullName, "| PR #:", issueNumber, "| action:", payload.Action)

		// Đã review xong đúng SHA này rồi — vd GitHub redeliver webhook,
		// hoặc "synchronize" bắn ra mà không thật sự có commit mới. Cùng cơ
		// chế dedup với luồng mention ở trên (issue #32).
		if review.AlreadyReviewedSHA(reviewStateStore, repoFullName, issueNumber, headSHA) {
			fmt.Println("Ignored: PR already reviewed at SHA", headSHA)
			fmt.Fprintln(w, "ignored")
			return
		}

		// Xin installation token đúng lúc này, sau khi mọi check rẻ đã qua —
		// cùng lý do với handleIssueComment ở trên (xem issue #47).
		token := installationToken(payload.Installation.ID)
		if _, err := token(); err != nil {
			fmt.Println("Get installation token error:", err)
			http.Error(w, "cannot authenticate with github", http.StatusInternalServerError)
			return
		}
		ghClient := githubapi.NewClientWithTokenFunc(token)

		placeholderID, err := ghClient.PostComment(repoFullName, issueNumber, "Đang review... _(tự động khi PR được mở/cập nhật)_")
		if err != nil {
			fmt.Println("Post comment error:", err)
			http.Error(w, "cannot post placeholder", http.StatusInternalServerError)
			return
		}

		dispatcher.Submit(newJob(ghClient, token, repoFullName, issueNumber, placeholderID, "review").Run)

		fmt.Fprintln(w, "processing")
	}

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

		// GitHub phân biệt loại event qua header X-GitHub-Event, KHÔNG phải
		// qua field "action" trong body (issue_comment và pull_request đều
		// có "action" nhưng giá trị/ý nghĩa khác nhau — xem webhook.Payload).
		switch eventType := r.Header.Get("X-GitHub-Event"); eventType {
		case "issue_comment":
			handleIssueComment(w, r, payload)
		case "pull_request":
			handlePullRequest(w, r, payload)
		default:
			fmt.Println("Ignored: unsupported X-GitHub-Event", eventType)
			fmt.Fprintln(w, "ignored")
		}
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
