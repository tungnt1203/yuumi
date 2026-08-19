# Yuumi Review Bot

Bot review code tự động: nhận mention `@yuumi-bot <lệnh>` trong comment trên GitHub PR/Issue, gọi Claude Code CLI để review, rồi tự động post kết quả lại thành comment trên đúng PR/Issue đó.

## Kiến trúc

```
GitHub PR/Issue comment "@yuumi-bot <lệnh>"
        │  (GitHub Webhook - HTTP POST, ký HMAC-SHA256)
        ▼
  Go HTTP server (cmd/server)
        │  verify chữ ký → check quyền → parse comment → trích lệnh
        ▼
  Gọi `claude -p` (Claude Code CLI) để review          [internal/claudecli]
        │
        ▼
  Post kết quả thành comment lên GitHub PR/Issue        [internal/githubapi]
```

## Cấu trúc thư mục

```
cmd/server/main.go        # entry point: load config, đăng ký route, start server
internal/
  config/                 # đọc & validate biến môi trường
  review/                 # Comment, MentionsBot, ExtractCommand (logic thuần, có unit test)
  webhook/                # Payload struct, VerifySignature (HMAC)
  claudecli/              # gọi `claude` CLI, parse kết quả
  githubapi/               # gọi GitHub REST API để post comment
```

## Yêu cầu

- Go 1.26+
- [Claude Code CLI](https://docs.claude.com/claude-code) đã cài và authenticate (`claude --version` chạy được)
- 1 GitHub Personal Access Token (fine-grained, quyền `Issues: Read and write` trên repo mục tiêu)
- 1 webhook secret tự đặt (dùng để GitHub ký request, verify chống giả mạo)

## Cấu hình

Tạo file `.env` ở thư mục gốc (đã có trong `.gitignore`, **không commit file này**):

```
GITHUB_TOKEN=<personal access token>
GITHUB_WEBHOOK_SECRET=<secret bạn tự đặt, khai báo trùng khi setup webhook trên GitHub>
ALLOWED_USERS=<username1,username2,...>   # danh sách GitHub username được phép trigger bot
```

## Chạy local

```bash
set -a && source .env && set +a
go run ./cmd/server
```

Server lắng nghe cổng `:8080`, có 2 route:

- `GET /health` — health check, trả `ok`.
- `POST /webhook` — endpoint nhận GitHub webhook (event `issue_comment`).

## Test thủ công (giả lập webhook GitHub)

```bash
BODY='{"action":"created","comment":{"body":"@yuumi-bot review","user":{"login":"<username>"}},"repository":{"full_name":"<owner>/<repo>"},"issue":{"number":<so>}}'
SIG=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" | sed 's/^.* //')
curl -i -X POST localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-Hub-Signature-256: sha256=$SIG" \
  -d "$BODY"
```

## Trạng thái

- [x] HTTP server nhận & verify webhook (HMAC-SHA256)
- [x] Xác thực người comment (allowlist)
- [x] Gọi Claude Code CLI review (có timeout, capture stderr)
- [x] Post kết quả lên GitHub PR/Issue (goroutine, không block response)
- [x] Chống panic làm sập server (`recover`)
- [x] Tái cấu trúc theo layout `cmd/` + `internal/`
- [ ] Unit test (`go test`)
- [ ] Đóng gói Docker
- [ ] Deploy AWS
