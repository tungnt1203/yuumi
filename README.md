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
  healthcheck/              # check claude CLI + GITHUB_TOKEN còn dùng được, cache cho /health
  reviewstate/              # lưu SHA đã review lần gần nhất cho mỗi PR, để review lần sau chỉ lấy phần đổi mới
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

## Rule mặc định theo loại file

Ngoài `instructions` của `.yuumi.yml` (repo tự khai báo), bot tự có sẵn 1 bộ rule mặc định gắn theo đuôi file có trong diff — không cần repo nào cấu hình gì cả:

- **Go**: race condition, error wrapping (`%w`), context leak, goroutine leak.
- **JavaScript/TypeScript**: floating promise, lạm dụng `any`/`as`, thiếu kiểm tra null/undefined.
- **Python**: mutable default argument, `except` quá rộng, resource không dùng `with`.
- **SQL**: N+1 query, thiếu index, SQL injection do nối string.

Bundle có nhiều loại file khác nhau thì rule của TẤT CẢ loại có mặt đều được chèn vào (không chỉ loại chiếm đa số). File loại chưa có rule riêng vẫn review bình thường với hướng dẫn chung. Nếu repo có `instructions` riêng trong `.yuumi.yml`, hướng dẫn của repo được **ưu tiên hơn** khi có xung đột với rule mặc định ở đây.

## Tự động đọc `.gitignore` của repo

Ngoài `exclude` ở `.yuumi.yml`, bot còn tự đọc file `.gitignore` thật ở root repo được review và **gộp thêm** pattern trong đó vào danh sách loại trừ (cộng dồn với default + `.yuumi.yml`, không thay thế) — repo nào đã tự đánh dấu 1 thư mục/file là "không cần track" (`coverage/`, `.turbo/`, `*.log`...) thì bot cũng không review nhầm nó.

Chỉ hỗ trợ các case phổ biến nhất, không phải toàn bộ spec `.gitignore`: comment/dòng trống/pattern phủ định (`!...`) bị bỏ qua, pattern có `/` (thư mục hoặc path lồng nhau) và pattern basename/đuôi file cố định hoạt động bình thường, wildcard đơn giản dạng `*.ext` cũng dịch được — wildcard phức tạp hơn (`file?.txt`, `[a-z]*`...) bị bỏ qua (không cố dịch sai). Không có `.gitignore` hoặc đọc lỗi đều không chặn review.

## Review lần 2 trở đi chỉ xem phần thay đổi mới

Mỗi lần review xong, bot ghi lại SHA vừa review cho đúng PR đó (`internal/reviewstate`, mặc định `logs/review-state.json`, override qua `REVIEW_STATE_FILE`). Lần review kế tiếp trên **cùng PR** (vd tác giả push thêm commit rồi mention lại `@yuumi-bot review`) sẽ tự lấy diff qua GitHub compare API (`GET /compare/{sha_cũ}...{sha_mới}`) — chỉ chứa phần thay đổi MỚI — thay vì gửi lại toàn bộ diff so với base như trước, giúp tiết kiệm token/thời gian gọi Claude CLI đáng kể trên PR có nhiều vòng review.

- Lần đầu review 1 PR (chưa có state) vẫn hoạt động như cũ: lấy full diff so với base.
- Lấy state hoặc gọi compare API lỗi đều fallback về full diff, không chặn review.
- Review lỗi (Claude CLI lỗi, ...) thì SHA đó **không** được ghi nhận là đã review — lần sau vẫn tính từ SHA đã review thành công gần nhất, tránh bỏ sót phần code chưa thực sự được xem qua.
- Comment sẽ có ghi chú `_(Chỉ review phần thay đổi mới so với lần review trước...)_` để người đọc biết bot có tối ưu, không phải review sót.

## Kết quả review có phân loại + comment inline theo đúng dòng code

Claude được yêu cầu trả kết quả dưới dạng JSON array các "finding" (`category`, `severity`, `message`, `suggestion`, kèm `file`/`line` nếu áp dụng được cho 1 dòng cụ thể) thay vì 1 khối text tự do.

- **Finding có `file`/`line` khớp đúng với diff thật** (đối chiếu lại bằng cách tự parse hunk của diff, không tin thẳng Claude) → post thành **comment inline** gắn đúng vào dòng đó qua GitHub Reviews API (`POST /pulls/{number}/reviews`), thay vì 1 dòng text lẫn trong comment tổng.
- **Finding không có `file`/`line`, hoặc có nhưng không khớp được với diff thật** (Claude "bịa" file/dòng không tồn tại, hoặc diễn giải lại thay vì copy nguyên văn số dòng) → vẫn hiển thị trong comment tổng hợp (placeholder), phân loại rõ theo severity (🔴 critical/🟠 high/🟡 medium/🔵 low) kèm category và gợi ý sửa nếu có — không bao giờ bị mất hay post sai chỗ trong im lặng.
- Claude trả text không đúng format JSON (bất chấp hướng dẫn) → fallback hiển thị nguyên văn như comment tổng hợp, review không bị coi là lỗi chỉ vì sai định dạng output.
- Post inline comment là bước **best-effort**, tách riêng khỏi comment tổng hợp: lỗi ở bước này (rate limit, lỗi mạng...) chỉ log lại, không làm mất kết quả review đã post thành công ở comment chính.

## Chạy local

```bash
set -a && source .env && set +a
go run ./cmd/server
```

Server lắng nghe cổng `:8080`, có 2 route:

- `GET /health` — trả trạng thái thật của các dependency (check lúc khởi động, cache lại, không gọi CLI/API mỗi request): `200` kèm JSON `{"claude_cli":{"ok":true,...},"github_token":{"ok":true,...},"checked_at":"..."}` nếu mọi thứ OK, `503` nếu có dependency lỗi.
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
- [x] `/health` phản ánh đúng trạng thái claude CLI + GITHUB_TOKEN (check lúc khởi động, cache lại) thay vì luôn trả "ok"
- [x] Tự đọc `.gitignore` thật của repo được review, gộp thêm vào danh sách loại trừ (cộng dồn với default + `.yuumi.yml`, không thay thế)
- [x] Review lần 2 trở đi trên cùng 1 PR chỉ gửi diff phần thay đổi mới (so với SHA đã review lần trước), không gửi lại toàn bộ diff cũ
- [x] Kết quả review có `category`/`severity`/gợi ý sửa (JSON có cấu trúc thay vì text tự do), finding gắn đúng vào dòng code qua GitHub Reviews API khi xác định được vị trí, còn lại hiển thị trong comment tổng hợp
- [x] Rule mặc định theo loại file (Go/JS/TS/Python/SQL) tự động chèn vào prompt theo đuôi file có trong diff, không cần repo cấu hình gì — ưu tiên thấp hơn `instructions` riêng của repo nếu có xung đột

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
