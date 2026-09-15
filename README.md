# Yuumi Review Bot

Bot review code tự động: nhận mention `@yuumi-bot <lệnh>` trong comment trên GitHub PR/Issue, gọi Claude Code CLI để review, rồi tự động post kết quả lại thành comment trên đúng PR/Issue đó.

## Kiến trúc

```
GitHub PR comment "@yuumi-bot <lệnh>"
        │  (GitHub Webhook - HTTP POST, ký HMAC-SHA256)
        ▼
  Go HTTP server (cmd/server)
        │  verify chữ ký → check quyền → parse comment → trích lệnh
        ▼
  React 👀 + post comment placeholder "Đang review..."  [internal/githubapi]
        │
        ▼
  Lấy PR head SHA → git clone --depth 1 vào tmp dir      [internal/githubapi, internal/gitrepo]
        │
        ▼
  Gọi `claude -p` với cmd.Dir = tmp dir để review        [internal/claudecli]
        │  (dọn tmp dir sau khi xong)
        ▼
  Edit lại đúng comment placeholder với kết quả/lỗi      [internal/githubapi]
```

## Cấu trúc thư mục

```
cmd/server/main.go        # entry point: load config, đăng ký route, start server
internal/
  config/                 # đọc & validate biến môi trường
  review/                 # Comment, MentionsBot, ExtractCommand (logic thuần, có unit test)
  webhook/                # Payload struct, VerifySignature (HMAC)
  claudecli/              # gọi `claude` CLI (chạy trong repo đã clone), parse kết quả
  githubapi/               # gọi GitHub REST API: reaction, post/edit comment, lấy PR head SHA
  gitrepo/                 # clone PR head SHA vào tmp dir, trả cleanup() để dọn dẹp
```

## Yêu cầu

- Go 1.26+
- [Claude Code CLI](https://docs.claude.com/claude-code) đã cài và authenticate (`claude --version` chạy được)
- `git` CLI có sẵn trên máy chạy server (dùng để clone PR head vào tmp dir)
- 1 GitHub Personal Access Token (fine-grained, quyền `Issues: Read and write` + `Pull requests: Read` trên repo mục tiêu)
- 1 webhook secret tự đặt (dùng để GitHub ký request, verify chống giả mạo)

## Cấu hình

Tạo file `.env` ở thư mục gốc (đã có trong `.gitignore`, **không commit file này**):

```
GITHUB_TOKEN=<personal access token>
GITHUB_WEBHOOK_SECRET=<secret bạn tự đặt, khai báo trùng khi setup webhook trên GitHub>
ALLOWED_USERS=<username1,username2,...>   # danh sách GitHub username được phép trigger bot
```

## Cấu hình review riêng cho từng repo (`.yuumi.yml`)

Repo được review có thể thêm file `.yuumi.yml` ở thư mục gốc để tuỳ chỉnh cách bot review repo đó, không cần đụng vào code/cấu hình của bot:

```yaml
exclude:
  - "testdata/"
  - "*.generated.go"
instructions: |
  Review nghiêm khắc phần error handling.
  Luôn yêu cầu unit test cho hàm export.
```

- `exclude`: thêm pattern loại trừ file/thư mục khỏi diff review — **gộp thêm** vào danh sách mặc định của bot (lock file, `vendor/`, `node_modules/`...), không thay thế.
- `instructions`: đoạn hướng dẫn chèn thẳng vào prompt gửi Claude, để review đúng convention/mức độ nghiêm khắc riêng của repo.

Không có file này thì bot dùng default hiện tại. File có nhưng sai định dạng YAML thì bot bỏ qua (log lỗi, không chặn review) và vẫn review với default.

## Chạy local

```bash
set -a && source .env && set +a
go run ./cmd/server
```

Server lắng nghe cổng `:8080`, có 2 route:

- `GET /health` — health check, trả `ok`.
- `POST /webhook` — endpoint nhận GitHub webhook (event `issue_comment`).

**Lưu ý:** `issue.number` trong payload phải là số của 1 **Pull Request thật** (không phải Issue thường), vì bước lấy head SHA gọi API `/pulls/{number}` — trên Issue thường API này trả 404.

## Test thủ công (giả lập webhook GitHub)

```bash
BODY='{"action":"created","comment":{"id":1,"body":"@yuumi-bot review","user":{"login":"<username>"}},"repository":{"full_name":"<owner>/<repo>"},"issue":{"number":<số PR>}}'
SIG=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" | sed 's/^.* //')
curl -i -X POST localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-Hub-Signature-256: sha256=$SIG" \
  -d "$BODY"
```

(`comment.id` là giả nên bước react 👀 sẽ luôn báo lỗi 404 — bình thường, không chặn các bước sau)

## Trạng thái

- [x] HTTP server nhận & verify webhook (HMAC-SHA256)
- [x] Xác thực người comment (allowlist)
- [x] React 👀 lên comment trigger + post comment placeholder "Đang review..."
- [x] Lấy PR head SHA, `git clone --depth 1` vào tmp dir riêng mỗi request
- [x] Gọi Claude Code CLI review với `cmd.Dir` trỏ vào repo đã clone (không còn đọc nhầm repo `yuumi_review`)
- [x] Edit lại đúng comment placeholder với kết quả hoặc lỗi (không để treo), dọn tmp dir sau khi xong
- [x] Chống panic làm sập server (`recover`)
- [x] Tái cấu trúc theo layout `cmd/` + `internal/`
- [x] Lấy diff thật của PR qua GitHub API (`application/vnd.github.v3.diff`) và đưa vào prompt, kèm hướng dẫn Claude đọc thêm file/README liên quan để hiểu kiến trúc & convention trước khi review, thay vì chỉ nhìn diff cô lập (`review.BuildReviewPrompt`)
- [x] Cấu hình review riêng cho từng repo qua file `.yuumi.yml` ở root repo được review (thêm pattern loại trừ, hướng dẫn review riêng)

**Đã fix limitation cũ:** trước đây clone `--depth 1` nên Claude không `git diff` được, chỉ đoán qua commit message. Giờ diff thật lấy trực tiếp từ GitHub API (không phụ thuộc git history), nên vẫn giữ `--depth 1` khi clone bình thường (chỉ cần file state để Claude đọc code, không cần history) — nếu gọi GitHub API lỗi thì fallback về cách cũ (đọc file + commit message).

### Roadmap tiếp theo (ưu tiên hoàn thiện app trước khi đổi kiến trúc)

1. [x] Unit test (`go test`) cho phần logic thuần (`review`, `webhook`)
   - [x] Lấy diff thật của PR qua GitHub API, đưa vào prompt review (`internal/review/prompt.go`)
2. [ ] Deploy có URL public thật (thay vì chỉ test local qua curl) — vẫn dùng PAT trước cho chắc chắn hoạt động
3. [ ] Chuyển từ PAT cá nhân sang **GitHub App** — để bot có identity riêng (`yuumi-bot[bot]`), token theo installation thay vì gắn với account cá nhân, scope đúng theo repo cài app. Việc cần làm:
   - Đăng ký GitHub App trên GitHub (permissions `Issues: RW`, `Pull requests: R`, subscribe event `issue_comment` + sau này `pull_request`)
   - Thêm module ký JWT bằng private key của App + đổi lấy installation access token (`POST /app/installations/{id}/access_tokens`), cache tới khi hết hạn
   - Đổi `config.Load()`: bỏ `GITHUB_TOKEN` tĩnh, dùng `GITHUB_APP_ID` + `GITHUB_APP_PRIVATE_KEY`
   - Thêm field `installation.id` vào `webhook/payload.go`
   - `gitrepo.CloneRepo` cần nhúng token vào URL khi fetch nếu sau này review repo private (hiện chỉ work với repo public)
4. [ ] Đóng gói Docker
5. [ ] Deploy AWS

**Để sau (đã bàn, chưa ưu tiên):**
- [ ] Auto review khi PR mới tạo / có commit mới (event `pull_request`, không chỉ mention)
- [ ] Migrate sang Go SDK (Tool Runner) thay vì shell ra `claude` CLI
