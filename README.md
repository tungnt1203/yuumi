# Yuumi Review Bot

Bot review code tự động: nhận mention `@yuumi-bot <lệnh>` trong comment trên GitHub PR/Issue, hoặc tự động chạy khi 1 PR mới mở/có commit mới (xem [Auto review](#auto-review-khi-pr-mới-mở--có-commit-mới)), gọi Claude Code CLI để review, rồi tự động post kết quả lại thành comment trên đúng PR đó.

## Kiến trúc

```
GitHub PR comment "@yuumi-bot <lệnh>"        PR mới mở / có commit mới
        │  (event issue_comment)                 │  (event pull_request)
        │  (GitHub Webhook - HTTP POST, ký HMAC-SHA256, phân biệt qua header X-GitHub-Event)
        ▼                                         ▼
  Go HTTP server (cmd/server)
        │  verify chữ ký → check quyền (allowlist) → route theo loại event
        ▼
  React 👀 (chỉ luồng mention) + post comment placeholder "Đang review..."  [internal/githubapi]
        │
        ▼
  Lấy PR head SHA → git clone --depth 1 vào tmp dir      [internal/githubapi, internal/gitrepo]
        │
        ▼
  Lấy diff thật của PR (full, hoặc chỉ phần mới nếu đã review trước đó)
  → lọc file rác → chia bundle theo thư mục → dựng prompt [internal/review]
        │
        ▼
  Gọi `claude -p` với cmd.Dir = tmp dir để review        [internal/claudecli]
  (mỗi bundle 1 lần gọi, tự retry khi lỗi tạm thời; dọn tmp dir sau khi xong)
        │
        ▼
  Edit lại đúng comment placeholder với kết quả/lỗi, post finding inline
  đúng dòng code qua Reviews API                         [internal/githubapi]
```

## Cấu trúc thư mục

```
cmd/
  server/main.go          # entry point: load config, đăng ký route, start server
  evalrun/main.go         # CLI chạy eval suite đo chất lượng review (xem mục Eval suite)
internal/
  config/                 # đọc & validate biến môi trường
  review/                 # toàn bộ logic review: Job (điều phối 1 lần review), prompt, chia bundle diff,
                          # parse finding/hunk, comment inline, rule theo ngôn ngữ, .yuumi.yml, .gitignore,
                          # Dispatcher (giới hạn job đồng thời), dedupe theo SHA — logic thuần, có unit test
  webhook/                # Payload struct, VerifySignature (HMAC), SeenComments (chống xử lý trùng comment)
  claudecli/              # gọi `claude` CLI (chạy trong repo đã clone), retry, parse kết quả
  githubapi/              # gọi GitHub REST API: reaction, post/edit comment, diff, compare, Reviews API
  gitrepo/                # clone PR head SHA vào tmp dir, trả cleanup() để dọn dẹp
  healthcheck/            # check claude CLI + GITHUB_TOKEN còn dùng được, cache cho /health
  reviewstate/            # lưu SHA đã review lần gần nhất cho mỗi PR, để review lần sau chỉ lấy phần đổi mới
  reviewlog/              # ghi log JSON (prompt/response/lỗi/thời gian) mỗi lần gọi Claude CLI
  evalrunner/             # dựng diff từ fixture before/after rồi chạy review, phục vụ cmd/evalrun
evalsuite/                # fixture bug cài sẵn + results.md theo dõi chất lượng review qua thời gian
.github/workflows/ci.yml  # CI: go build + go vet + go test trên mỗi PR và push vào main
```

## Yêu cầu

- Go 1.26+ (xem `go.mod` / `.tool-versions`)
- [Claude Code CLI](https://docs.claude.com/claude-code) đã cài và authenticate (`claude --version` chạy được)
- `git` CLI có sẵn trên máy chạy server (dùng để clone PR head vào tmp dir)
- 1 GitHub Personal Access Token (fine-grained, quyền `Issues: Read and write` + `Pull requests: Read and write` trên repo mục tiêu — cần write vì bot post finding inline qua Reviews API)
- 1 webhook secret tự đặt (dùng để GitHub ký request, verify chống giả mạo)

## Cấu hình

Tạo file `.env` ở thư mục gốc (đã có trong `.gitignore`, **không commit file này**):

```
GITHUB_TOKEN=<personal access token>
GITHUB_WEBHOOK_SECRET=<secret bạn tự đặt, khai báo trùng khi setup webhook trên GitHub>
ALLOWED_USERS=<username1,username2,...>   # danh sách GitHub username được phép trigger bot
```

3 biến trên là **bắt buộc** (thiếu 1 biến server không khởi động). Ngoài ra có các biến **tuỳ chọn**, không set thì dùng default:

| Biến | Default | Ý nghĩa |
|------|---------|---------|
| `MAX_DIFF_BUNDLE_CHARS` | `12000` | Ngưỡng ký tự diff cho mỗi bundle (mỗi bundle = 1 lần gọi Claude). Phải là số nguyên dương. |
| `MAX_CONCURRENT_REVIEWS` | `3` | Số job review chạy đồng thời tối đa; job vượt mức sẽ chờ tới khi có slot trống. Phải là số nguyên dương. |
| `REVIEW_LOG_DIR` | `logs/reviews` | Thư mục ghi log mỗi lần gọi Claude CLI (xem mục Log review). |
| `REVIEW_STATE_FILE` | `logs/review-state.json` | File lưu SHA đã review gần nhất cho từng PR (xem mục review lần 2 trở đi). |

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

## Lọc file rác và chia bundle diff

Diff của PR được xử lý trước khi gửi cho Claude (`internal/review/diffsplit.go`):

1. **Lọc file không cần review** trước khi tính kích thước: lock file (`go.sum`, `package-lock.json`, `yarn.lock`...), thư mục sinh ra (`vendor/`, `node_modules/`, `dist/`, `build/`...), file minified/binary/ảnh/font. Danh sách này được gộp với `exclude` của `.yuumi.yml` và `.gitignore` của repo (xem các mục dưới). Comment kết quả có ghi chú `_(Đã bỏ qua N file không cần review: ...)_` để người đọc biết.
2. **Chia bundle**: diff còn lại được nhóm theo thư mục (file impl + test cùng thư mục đi cùng nhau) rồi chia thành các bundle không vượt `MAX_DIFF_BUNDLE_CHARS` (mặc định 12000 ký tự). Mỗi bundle là 1 lần gọi `claude -p` riêng, chạy **tuần tự**, sau đó kết quả được gộp lại vào đúng 1 comment. PR nhỏ chỉ có 1 bundle.
3. **Cảnh báo diff bị cắt**: nếu số file parse được từ diff ít hơn `changed_files` mà GitHub báo cho cả PR (GitHub tự cắt diff của PR quá lớn), comment sẽ có dòng `⚠️ GitHub chỉ trả về diff của X/Y file — review có thể sót file.`

## Độ ổn định: retry, giới hạn đồng thời, chống xử lý trùng

- **Retry**: `claude` CLI lỗi khi chạy lệnh (timeout 5 phút, lỗi mạng...) được thử lại tối đa 3 lần (gồm lần đầu) với backoff. Lỗi Claude tự báo (`is_error`) hoặc output không parse được **không** retry vì thử lại với cùng input không đổi được kết quả.
- **Giới hạn đồng thời**: `Dispatcher` chạy mỗi job trong 1 goroutine riêng nhưng chỉ cho tối đa `MAX_CONCURRENT_REVIEWS` job chạy cùng lúc (mỗi job spawn `git` + `claude` thật). HTTP handler luôn trả lời webhook ngay, không bị chặn bởi hàng đợi.
- **Chống xử lý trùng**: comment ID đã xử lý được nhớ trong memory (`webhook.SeenComments`), nên GitHub redeliver webhook hoặc mention trùng không tạo 2 job cùng edit 1 comment. Đây là lưu trong RAM, restart server thì mất và không có TTL — chấp nhận được với 1 instance nội bộ, cần lưu ngoài (Redis/DB) nếu chạy nhiều instance.
- **Không để treo**: mọi lỗi trong luồng review đều được ghi lại vào comment placeholder (`❌ Review thất bại: ...`), và panic trong handler được `recover` để không làm sập server.

## Check tĩnh trước khi review (repo Go)

Nếu repo được review có `go.mod`, bot chạy `gofmt` và `go vet` trên bản checkout và chèn báo cáo vào prompt, để Claude tập trung nhận xét logic/thiết kế thay vì lặp lại lỗi format/vet mà máy đã bắt được. Repo không phải Go thì bước này tự bỏ qua; chưa hỗ trợ tool của ngôn ngữ khác.

## Log mỗi lần review

Mỗi lần gọi Claude CLI được ghi thành 1 file JSON trong `logs/reviews/` (đổi bằng `REVIEW_LOG_DIR`) gồm: thời gian, repo, số PR, SHA, chỉ số bundle, **prompt và response nguyên văn**, lỗi (nếu có), thời gian xử lý (`duration_ms`), số lần thử (`attempts`) và số turn Claude dùng (`num_turns`, thấp bất thường trên bundle nhiều file là dấu hiệu Claude review mù trên diff). Dùng để truy vết khi review lỗi hoặc kết quả lạ. Lỗi ghi log chỉ in ra console, không chặn review. Thư mục `logs/` đã nằm trong `.gitignore`; prompt/response được ghi nguyên văn nên chú ý nếu code review chứa thông tin nhạy cảm.

## Rule mặc định theo loại file

Ngoài `instructions` của `.yuumi.yml` (repo tự khai báo), bot tự có sẵn 1 bộ rule mặc định gắn theo đuôi file có trong diff — không cần repo nào cấu hình gì cả:

- **Go**: race condition, error wrapping (`%w`), context leak, goroutine leak.
- **JavaScript/TypeScript**: floating promise, lạm dụng `any`/`as`, thiếu kiểm tra null/undefined.
- **Python**: mutable default argument, `except` quá rộng, resource không dùng `with`.
- **SQL**: N+1 query, thiếu index, SQL injection do nối string.

Bundle có nhiều loại file khác nhau thì rule của TẤT CẢ loại có mặt đều được chèn vào (không chỉ loại chiếm đa số). File loại chưa có rule riêng vẫn review bình thường với hướng dẫn chung. Nếu repo có `instructions` riêng trong `.yuumi.yml`, hướng dẫn của repo được **ưu tiên hơn** khi có xung đột với rule mặc định ở đây.

## Gợi ý symbol thay đổi để bắt breaking change ở package khác

Bot nhóm file review theo cùng thư mục (`groupByDirectory`), giải quyết tốt case impl + test cùng thư mục nhưng bỏ sót case 1 thay đổi ảnh hưởng file ở **package khác** (vd đổi signature 1 method nhưng nơi gọi nằm ở package khác — đặc biệt rủi ro với Go do interface ngầm định).

Để bù lại mà không cần parser AST đầy đủ: bot trích best-effort tên symbol (hàm/type/method) xuất hiện ở dòng thay đổi trong diff, liệt kê vào prompt kèm hướng dẫn Claude tự `grep`/tìm kiếm các tên đó ở nơi khác trong repo trước khi kết luận không có breaking change — thay vì chỉ dựa vào diff hoặc heuristic thư mục.

## Ngữ cảnh dùng chung giữa các phần khi PR bị chia bundle

Khi 1 PR lớn bị chia thành nhiều bundle (mỗi bundle là 1 lần gọi `claude -p` riêng, xem phần bundle ở trên), bot tổng hợp sẵn 1 lần trước khi chia:

- Danh sách **toàn bộ** file bị đổi trong PR (không chỉ file của riêng từng bundle).
- Đường dẫn README/convention doc gần nhất tìm được trong repo (ở root và ở thư mục của từng file thay đổi).

Ngữ cảnh này được nhúng y hệt vào đầu prompt của **mọi** bundle, thay cho việc chỉ dặn chung chung "hãy tự đọc thêm file liên quan" và để mỗi bundle tự quyết định lại — tránh mỗi phần của cùng 1 PR tự khám phá lại từ đầu (tốn turn/token) hoặc bỏ qua luôn (review thiếu ngữ cảnh, không nhất quán giữa các phần). PR không bị chia bundle (đa số) không tốn công build phần này.

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

## Auto review khi PR mới mở / có commit mới

Ngoài mention thủ công, bot còn tự chạy review khi nhận webhook event `pull_request` với action `opened` (PR mới tạo) hoặc `synchronize` (có commit mới push lên PR) — không cần ai gõ `@yuumi-bot review`.

- **Setup webhook trên GitHub**: ngoài event `Issue comments` đã cấu hình cho luồng mention, cần bật thêm event **`Pull requests`** (Settings → Webhooks → chọn repo → "Let me select individual events"). Server phân biệt 2 loại event qua header `X-GitHub-Event` (không dựa vào field `action` trong body, vì cả 2 event đều có field này nhưng ý nghĩa khác nhau).
- **Allowlist**: `ALLOWED_USERS` (biến môi trường vốn dùng để chặn ai được phép mention bot) được **tái dùng** cho auto-review — chỉ tự động review PR do chính tác giả (`pull_request.user.login`) nằm trong danh sách này tạo ra, để không tự ý review "miễn phí" mọi PR của bất kỳ ai gửi vào repo đã cài webhook.
- **Dùng chung logic review** với luồng mention: cả 2 luồng cùng dựng `review.Job` như nhau (chỉ khác cách lấy `RepoFullName`/`IssueNumber`/comment đầu vào), và cùng dùng cơ chế tra cứu "SHA đã review lần trước" (`review.AlreadyReviewedSHA`, xem mục review lần 2 trở đi ở trên) để tránh review trùng: PR đã được auto-review lúc mở, sau đó có người mention `@yuumi-bot review` lại đúng SHA đó (hoặc GitHub redeliver webhook trùng) sẽ bị bỏ qua thay vì tốn thêm 1 lần gọi Claude CLI cho việc không có gì mới.
- Action khác `opened`/`synchronize` của event `pull_request` (`closed`, `reopened`, `edited`, `labeled`...) không kích hoạt gì cả.

## Chạy local

```bash
set -a && source .env && set +a
go run ./cmd/server
```

Server lắng nghe cổng `:8080`, có 2 route:

- `GET /health` — trả trạng thái thật của các dependency (check lúc khởi động, cache lại, không gọi CLI/API mỗi request): `200` kèm JSON `{"claude_cli":{"ok":true,...},"github_token":{"ok":true,...},"checked_at":"..."}` nếu mọi thứ OK, `503` nếu có dependency lỗi.
- `POST /webhook` — endpoint nhận GitHub webhook (event `issue_comment` cho mention thủ công, `pull_request` cho auto-review — phân biệt qua header `X-GitHub-Event`, xem mục Auto review ở trên).

**Lưu ý:** `issue.number` trong payload phải là số của 1 **Pull Request thật** (không phải Issue thường), vì bước lấy head SHA gọi API `/pulls/{number}` — trên Issue thường API này trả 404.

## Test thủ công (giả lập webhook GitHub)

```bash
BODY='{"action":"created","comment":{"id":1,"body":"@yuumi-bot review","user":{"login":"<username>"}},"repository":{"full_name":"<owner>/<repo>"},"issue":{"number":<số PR>}}'
SIG=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" | sed 's/^.* //')
curl -i -X POST localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: issue_comment" \
  -H "X-Hub-Signature-256: sha256=$SIG" \
  -d "$BODY"
```

(`comment.id` là giả nên bước react 👀 sẽ luôn báo lỗi 404 — bình thường, không chặn các bước sau)

Giả lập auto-review (event `pull_request`, xem mục Auto review ở trên — `<username>` phải nằm trong `ALLOWED_USERS`):

```bash
BODY='{"action":"opened","repository":{"full_name":"<owner>/<repo>"},"pull_request":{"number":<số PR>,"head":{"sha":"<head sha>"},"user":{"login":"<username>"}}}'
SIG=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" | sed 's/^.* //')
curl -i -X POST localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: pull_request" \
  -H "X-Hub-Signature-256: sha256=$SIG" \
  -d "$BODY"
```

## Test thật với GitHub qua ngrok

Để thử với webhook GitHub thật khi chưa deploy (issue #46), mở tunnel từ máy local ra URL public bằng [ngrok](https://ngrok.com):

1. Chạy server: `set -a && source .env && set +a && go run ./cmd/server`, rồi kiểm tra `curl -i localhost:8080/health` trả `200`.
2. Mở tunnel ở terminal khác: `ngrok http 8080` (hoặc `ngrok http --url=<domain-cố-định> 8080` nếu có domain ngrok cố định, để không phải sửa lại webhook mỗi lần chạy lại). Có thể xem từng request GitHub gửi tới ở `http://127.0.0.1:4040`.
3. Trên repo đích: **Settings → Webhooks → Add webhook**:
   - **Payload URL**: `https://<domain-ngrok>/webhook` (viết liền, không có khoảng trắng, phải có `/webhook`).
   - **Content type**: `application/json`. Mặc định của GitHub là `application/x-www-form-urlencoded`, khi đó server trả `400 invalid JSON`.
   - **Secret**: đúng giá trị `GITHUB_WEBHOOK_SECRET` trong `.env` (sai secret server trả `401`).
   - **Events**: chọn "Let me select individual events" → **Issue comments** + **Pull requests**.
4. Tab **Recent Deliveries** của webhook: event `ping` đầu tiên phải có dấu tick xanh. Log server sẽ in `Ignored: unsupported X-GitHub-Event ping` — bình thường, server chỉ xử lý `issue_comment` và `pull_request`.
5. Comment `@yuumi-bot review` trên 1 **Pull Request thật** bằng tài khoản có trong `ALLOWED_USERS`, hoặc mở PR mới / push thêm commit để thử auto-review (tác giả PR phải nằm trong `ALLOWED_USERS`). Bot sẽ react 👀, hiện "Đang review...", rồi sửa comment đó thành kết quả review.

PAT trong `.env` cần quyền `Issues: Read and write` + `Pull requests: Read and write` **trên đúng repo đích**, nếu thiếu sẽ gặp 403.

## Unit test và CI

```bash
go build ./... && go vet ./... && go test ./...
```

3 lệnh này cũng là những gì CI (`.github/workflows/ci.yml`) chạy trên mỗi PR và mỗi push vào `main`. Unit test không cần token hay `claude` CLI.

## Eval suite: đo chất lượng review theo thời gian

`evalsuite/` chứa các fixture PR có bug cài sẵn (`sql-injection-go`, `go-goroutine-leak`, `js-floating-promise`, `python-mutable-default`) để trả lời câu hỏi "đổi prompt/model có làm review tốt hơn hay tệ đi?":

```bash
go run ./cmd/evalrun                    # chạy toàn bộ fixture
go run ./cmd/evalrun sql-injection-go   # chỉ 1 fixture
```

Cần `claude` CLI đã authenticate. Đây là công cụ chạy tay, **không** nằm trong `go test`/CI. Đối chiếu output với `expected.md` của từng fixture rồi ghi 1 dòng vào `evalsuite/results.md`. Chi tiết và cách thêm fixture: [evalsuite/README.md](./evalsuite/README.md).

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
- [x] Trích best-effort symbol (hàm/type/method) thay đổi trong diff, chèn vào prompt kèm hướng dẫn Claude tự grep tìm nơi dùng ở package khác trước khi kết luận không có breaking change
- [x] Tự động review khi PR mới mở hoặc có commit mới (event `pull_request`, action `opened`/`synchronize`), không chỉ khi được mention — allowlist theo tác giả PR, dùng chung `review.Job` và cơ chế dedup theo SHA với luồng mention
- [x] Lọc file rác (lock/vendor/generated/binary), chia bundle diff theo thư mục, cảnh báo khi GitHub tự cắt diff
- [x] Retry khi Claude CLI lỗi tạm thời, giới hạn số job review chạy đồng thời, chống xử lý trùng comment
- [x] Check tĩnh `gofmt`/`go vet` trước khi review repo Go, chèn báo cáo vào prompt
- [x] Ghi log JSON mỗi lần gọi Claude CLI (prompt/response/lỗi/thời gian/attempts/num_turns)
- [x] Chia sẻ ngữ cảnh/primer dùng chung giữa các bundle của cùng 1 PR
- [x] Eval suite (`evalsuite/`, `cmd/evalrun`) với 4 fixture bug cài sẵn để theo dõi chất lượng review theo thời gian
- [x] CI (`.github/workflows/ci.yml`): build + vet + test trên mỗi PR và push vào `main`

**Đã fix limitation cũ:** trước đây clone `--depth 1` nên Claude không `git diff` được, chỉ đoán qua commit message. Giờ diff thật lấy trực tiếp từ GitHub API (không phụ thuộc git history), nên vẫn giữ `--depth 1` khi clone bình thường (chỉ cần file state để Claude đọc code, không cần history) — nếu gọi GitHub API lỗi thì fallback về cách cũ (đọc file + commit message).

### Roadmap tiếp theo (ưu tiên hoàn thiện app trước khi đổi kiến trúc)

1. [x] Unit test (`go test`) cho phần logic thuần (`review`, `webhook`)
   - [x] Lấy diff thật của PR qua GitHub API, đưa vào prompt review (`internal/review/prompt.go`)
2. [ ] Deploy có URL public thật (thay vì chỉ test local qua curl) — vẫn dùng PAT trước cho chắc chắn hoạt động (issue #46). Đã thử được webhook GitHub thật qua ngrok (xem mục Test thật với GitHub qua ngrok); còn lại là deploy chạy lâu dài trên hạ tầng thật
3. [ ] Chuyển từ PAT cá nhân sang **GitHub App** (issue #47) — để bot có identity riêng (`yuumi-bot[bot]`), token theo installation thay vì gắn với account cá nhân, scope đúng theo repo cài app. Việc cần làm:
   - Đăng ký GitHub App trên GitHub (permissions `Issues: RW`, `Pull requests: RW`, subscribe event `issue_comment` + `pull_request`)
   - Thêm module ký JWT bằng private key của App + đổi lấy installation access token (`POST /app/installations/{id}/access_tokens`), cache tới khi hết hạn
   - Đổi `config.Load()`: bỏ `GITHUB_TOKEN` tĩnh, dùng `GITHUB_APP_ID` + `GITHUB_APP_PRIVATE_KEY`
   - Thêm field `installation.id` vào `webhook/payload.go`
   - `gitrepo.CloneRepo` cần nhúng token vào URL khi fetch nếu sau này review repo private (hiện chỉ work với repo public) — theo dõi riêng ở issue #48
4. [ ] Đóng gói Docker (issue #49)
5. [ ] Deploy AWS (issue #50)

**Để sau (đã bàn, chưa ưu tiên):**
- [ ] Migrate sang Go SDK (Tool Runner) thay vì shell ra `claude` CLI
