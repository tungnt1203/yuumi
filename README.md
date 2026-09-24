<div align="center">
  <img src=".github/assets/logo.jpeg" alt="Yuumi Review logo" width="160" />

  # Yuumi Review

  **Bot review code tự động cho GitHub PR, chạy Claude Code CLI đứng sau**

  [![CI](https://github.com/tungnt1203/yuumi/actions/workflows/ci.yml/badge.svg)](https://github.com/tungnt1203/yuumi/actions/workflows/ci.yml)
  [![Go Version](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
  [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

Nhận mention `@yuumi <lệnh>` trong comment trên GitHub PR/Issue, hoặc tự
động chạy khi 1 PR mới mở/có commit mới, gọi [Claude Code CLI](https://docs.claude.com/claude-code)
để review, rồi post kết quả lại thành comment + inline comment đúng dòng code
trên chính PR đó.

## Mục lục

- [Tính năng](#tính-năng)
- [Kiến trúc](#kiến-trúc)
- [Cấu trúc thư mục](#cấu-trúc-thư-mục)
- [Yêu cầu](#yêu-cầu)
- [Cài đặt & cấu hình](#cài-đặt--cấu-hình)
- [Cấu hình review riêng cho từng repo (`.yuumi.yml`)](#cấu-hình-review-riêng-cho-từng-repo-yuumiyml)
- [Chạy local](#chạy-local)
- [Cách hoạt động chi tiết](#cách-hoạt-động-chi-tiết)
- [Eval suite](#eval-suite)
- [Roadmap](#roadmap)
- [Đóng góp](#đóng-góp)
- [License](#license)

## Tính năng

- **Review qua mention hoặc tự động**: comment `@yuumi review`, hoặc bot tự chạy khi PR mới mở/có commit mới (allowlist theo tác giả).
- **Xác thực qua GitHub App**: JWT RS256 + installation access token (tự cache/làm mới), bot có identity riêng `<tên App>[bot]`, không gắn với tài khoản cá nhân.
- **Finding có phân loại + comment inline**: kết quả trả về JSON có `category`/`severity`/gợi ý sửa; finding khớp đúng dòng diff được post inline qua Reviews API, còn lại gộp vào comment tổng hợp.
- **Rule mặc định theo ngôn ngữ**: Go, JavaScript/TypeScript, Python, SQL — tự chèn vào prompt theo đuôi file có trong diff, không cần repo cấu hình gì; rule soát secret/credential hardcode áp dụng cho mọi file.
- **Cấu hình riêng theo repo** qua `.yuumi.yml` (loại trừ file, hướng dẫn review riêng), tự đọc thêm `.gitignore` của repo.
- **Chỉ review phần thay đổi mới** ở các lần review sau trên cùng 1 PR (so với SHA đã review trước), tiết kiệm token.
- **Lọc file rác & chia bundle diff** theo thư mục để không vượt giới hạn ký tự mỗi lần gọi Claude, PR lớn vẫn review đầy đủ.
- **Ổn định khi chạy thật**: retry lỗi tạm thời, giới hạn job đồng thời, chống xử lý trùng comment, log JSON mỗi lần gọi Claude CLI.
- **Eval suite** (`evalsuite/`) để đo chất lượng review theo thời gian mỗi khi đổi prompt/model.

## Kiến trúc

```
GitHub PR comment "@yuumi <lệnh>"        PR mới mở / có commit mới
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
  reviewstats/main.go     # CLI tổng hợp token/chi phí từ review log theo repo và theo ngày
internal/
  config/                 # đọc & validate biến môi trường
  review/                 # toàn bộ logic review: Job (điều phối 1 lần review), prompt, chia bundle diff,
                          # parse finding/hunk, comment inline, rule theo ngôn ngữ, .yuumi.yml, .gitignore,
                          # Dispatcher (giới hạn job đồng thời), dedupe theo SHA — logic thuần, có unit test
  webhook/                # Payload struct, VerifySignature (HMAC), SeenComments (chống xử lý trùng comment)
  claudecli/              # gọi `claude` CLI (chạy trong repo đã clone), retry, parse kết quả
  githubapi/              # gọi GitHub REST API: reaction, post/edit comment, diff, compare, Reviews API
  gitrepo/                # clone PR head SHA vào tmp dir, trả cleanup() để dọn dẹp
  healthcheck/            # check claude CLI + GitHub App auth còn dùng được, cache cho /health
  githubapp/              # xác thực GitHub App: ký JWT, đổi/cache installation access token
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
- 1 [GitHub App](https://github.com/settings/apps) đã đăng ký, quyền `Issues: Read and write` + `Pull requests: Read and write` (cần write vì bot post finding inline qua Reviews API), subscribe event `Issue comments` + `Pull request`, đã **cài (Install App)** vào repo mục tiêu
- 1 webhook secret tự đặt (dùng để GitHub ký request, verify chống giả mạo — khai báo trong cấu hình webhook của chính App, không phải trên từng repo)

## Cài đặt & cấu hình

Tạo file `.env` ở thư mục gốc (đã có trong `.gitignore`, **không commit file này**):

```
GITHUB_APP_ID=<App ID, xem trang settings của App>
GITHUB_APP_PRIVATE_KEY_PATH=<đường dẫn tới file .pem tải về lúc tạo App>   # dùng khi chạy local
# HOẶC (thay vì _PATH ở trên) — tiện hơn khi deploy (Docker/AWS...), tránh lỗi escape newline của PEM:
# GITHUB_APP_PRIVATE_KEY=<nội dung file .pem encode base64 thành 1 dòng, vd: base64 < private-key.pem>
GITHUB_WEBHOOK_SECRET=<secret bạn tự đặt, khai báo trùng trong cấu hình webhook của App>
ALLOWED_USERS=<username1,username2,...>   # danh sách GitHub username được phép trigger bot
```

`GITHUB_APP_ID`, `GITHUB_WEBHOOK_SECRET`, `ALLOWED_USERS`, và **1 trong 2** biến private key ở trên là **bắt buộc** (thiếu là server không khởi động). Ngoài ra có các biến **tuỳ chọn**, không set thì dùng default:

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

## Chạy local

```bash
set -a && source .env && set +a
go run ./cmd/server
```

Server lắng nghe cổng `:8080`, có route `GET /health` và `POST /webhook`. Để
test webhook bằng curl hoặc test thật với GitHub qua ngrok, xem
[CONTRIBUTING.md](./CONTRIBUTING.md).

## Cách hoạt động chi tiết

<details id="lọc-file-rác-và-chia-bundle-diff">
<summary><strong>Lọc file rác và chia bundle diff</strong></summary>

Diff của PR được xử lý trước khi gửi cho Claude (`internal/review/diffsplit.go`):

1. **Lọc file không cần review** trước khi tính kích thước: lock file (`go.sum`, `package-lock.json`, `yarn.lock`...), thư mục sinh ra (`vendor/`, `node_modules/`, `dist/`, `build/`...), file minified/binary/ảnh/font. Danh sách này được gộp với `exclude` của `.yuumi.yml` và `.gitignore` của repo. Comment kết quả có ghi chú `_(Đã bỏ qua N file không cần review: ...)_` để người đọc biết.
2. **Chia bundle**: diff còn lại được nhóm theo thư mục (file impl + test cùng thư mục đi cùng nhau) rồi chia thành các bundle không vượt `MAX_DIFF_BUNDLE_CHARS` (mặc định 12000 ký tự). Mỗi bundle là 1 lần gọi `claude -p` riêng, chạy **tuần tự**, sau đó kết quả được gộp lại vào đúng 1 comment. PR nhỏ chỉ có 1 bundle.
3. **Cảnh báo diff bị cắt**: nếu số file parse được từ diff ít hơn `changed_files` mà GitHub báo cho cả PR (GitHub tự cắt diff của PR quá lớn), comment sẽ có dòng `⚠️ GitHub chỉ trả về diff của X/Y file — review có thể sót file.`

</details>

<details id="độ-ổn-định-retry-giới-hạn-đồng-thời-chống-xử-lý-trùng">
<summary><strong>Độ ổn định: retry, giới hạn đồng thời, chống xử lý trùng</strong></summary>

- **Retry**: `claude` CLI lỗi khi chạy lệnh (timeout 5 phút, lỗi mạng...) được thử lại tối đa 3 lần (gồm lần đầu) với backoff. Lỗi Claude tự báo (`is_error`) hoặc output không parse được **không** retry vì thử lại với cùng input không đổi được kết quả.
- **Giới hạn đồng thời**: `Dispatcher` chạy mỗi job trong 1 goroutine riêng nhưng chỉ cho tối đa `MAX_CONCURRENT_REVIEWS` job chạy cùng lúc (mỗi job spawn `git` + `claude` thật). HTTP handler luôn trả lời webhook ngay, không bị chặn bởi hàng đợi.
- **Chống xử lý trùng**: comment ID đã xử lý được nhớ trong memory (`webhook.SeenComments`), nên GitHub redeliver webhook hoặc mention trùng không tạo 2 job cùng edit 1 comment. ID được đánh dấu ngay khi nhận webhook để hai request cùng lúc không chạy hai job. Nếu lấy token hoặc post placeholder lỗi, dấu đó được xóa và handler trả `500` để GitHub gửi lại. Đây là lưu trong RAM, restart server thì mất và không có TTL — chấp nhận được với 1 instance nội bộ, cần lưu ngoài (Redis/DB) nếu chạy nhiều instance.
- **Xử lý lỗi**: lỗi gọi Claude CLI được ghi vào comment placeholder dưới dạng `❌ Review thất bại: ...` (nội dung lỗi của chính lần gọi review). Lỗi lấy head SHA, lỗi clone, và panic (được `recover` để không làm sập server) chỉ sửa placeholder thành một câu chung, chi tiết nằm trong log server — thông báo lỗi gốc có thể chứa đường dẫn máy hoặc token. Nếu chính lệnh sửa comment đó cũng lỗi, placeholder có thể vẫn treo và nguyên nhân chỉ còn trong log server.

</details>

<details id="check-tĩnh-trước-khi-review-repo-go">
<summary><strong>Check tĩnh trước khi review (repo Go)</strong></summary>

Nếu repo được review có `go.mod`, bot chạy `gofmt` và `go vet` trên bản checkout và chèn báo cáo vào prompt, để Claude tập trung nhận xét logic/thiết kế thay vì lặp lại lỗi format/vet mà máy đã bắt được. Repo không phải Go thì bước này tự bỏ qua; chưa hỗ trợ tool của ngôn ngữ khác.

</details>

<details id="log-mỗi-lần-review">
<summary><strong>Log mỗi lần review</strong></summary>

Mỗi lần gọi Claude CLI được ghi thành 1 file JSON trong `logs/reviews/` (đổi bằng `REVIEW_LOG_DIR`) gồm: thời gian, repo, số PR, SHA, chỉ số bundle, **prompt và response nguyên văn**, lỗi (nếu có), thời gian xử lý (`duration_ms`), số lần thử (`attempts`) và số turn Claude dùng (`num_turns`, thấp bất thường trên bundle nhiều file là dấu hiệu Claude review mù trên diff) và `usage` — token (`input_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `output_tokens`) cùng `cost_usd` do Claude CLI báo, cộng dồn qua các lần retry. Phần lớn input nằm ở 2 field cache, `input_tokens` chỉ là phần không qua cache. Dùng để truy vết khi review lỗi hoặc kết quả lạ. Lỗi ghi log chỉ in ra console, không chặn review. Thư mục `logs/` đã nằm trong `.gitignore`; prompt/response được ghi nguyên văn nên chú ý nếu code review chứa thông tin nhạy cảm.

Xem tổng số lần gọi, token và chi phí theo repo và theo ngày (giờ local của máy chạy lệnh):

```bash
go run ./cmd/reviewstats                 # đọc $REVIEW_LOG_DIR hoặc logs/reviews
go run ./cmd/reviewstats -dir <dir> -json
```

Log ghi trước khi có `usage` vẫn được đếm số lần gọi, token/chi phí tính là 0; `reviewstats` báo số lần gọi như vậy (`no_usage` trong `-json`). Lần thử bị timeout/kill giữa chừng không có output nên không đếm được token, chi phí thực tế có thể cao hơn một chút.

</details>

<details id="rule-mặc-định-theo-loại-file">
<summary><strong>Rule mặc định theo loại file</strong></summary>

Ngoài `instructions` của `.yuumi.yml` (repo tự khai báo), bot tự có sẵn 1 bộ rule mặc định gắn theo đuôi file có trong diff — không cần repo nào cấu hình gì cả:

- **Go**: race condition, error wrapping (`%w`), context leak, goroutine leak.
- **JavaScript/TypeScript**: floating promise, lạm dụng `any`/`as`, thiếu kiểm tra null/undefined.
- **Python**: mutable default argument, `except` quá rộng, resource không dùng `with`.
- **SQL**: N+1 query, thiếu index, SQL injection do nối string.
- **Mọi loại file**: secret/credential hardcode (API key, password, token, private key, connection string có password). Trước khi gọi Claude, bot còn quét regex nhanh các dòng **thêm mới** trong diff (AWS key `AKIA...`, header PEM private key, GitHub/Slack token, `sk-...`, biến tên chứa `password`/`passphrase`/`secret`/`api_key`/`access_token`/`auth_token` gán string literal ≥ 6 ký tự (`=`, `:=`, `:`) — trừ giá trị chỉ là tên env var như `"DB_PASSWORD"`, hoặc key là tham chiếu: `secretName`/`secretKeyRef`, key kết thúc bằng `file`/`path` sau dấu `_`/`.`/`-` hoặc ranh giới camelCase (`DB_PASSWORD_FILE`, `secretPath` — không tính `secretProfile`), key `*Header` khi giá trị cũng là tên header (`"X-Api-Key"`); dạng không quote `DB_PASSWORD=...` (kể cả `- KEY=...`, comment ` #` cuối dòng, `#` nằm trong giá trị vẫn tính là giá trị) trong file config/`.env`; `scheme://user:pass@host`) và chèn danh sách `file:dòng` khớp vào prompt để Claude xác minh — chỉ ghi vị trí + loại, không chép lại giá trị secret. Rule này là **bắt buộc**: `instructions` trong `.yuumi.yml` không tắt được (file đó đọc từ head của PR, tác giả PR sửa được), chỉ bổ sung được ngoại lệ cho từng đường dẫn file cụ thể (vd 1 file fixture/test); ngoại lệ phủ cả thư mục/pattern bị bỏ qua.

Bundle có nhiều loại file khác nhau thì rule của TẤT CẢ loại có mặt đều được chèn vào (không chỉ loại chiếm đa số). File loại chưa có rule riêng vẫn review bình thường với hướng dẫn chung. Nếu repo có `instructions` riêng trong `.yuumi.yml`, hướng dẫn của repo được **ưu tiên hơn** khi có xung đột với rule mặc định ở đây.

</details>

<details id="gợi-ý-symbol-thay-đổi-để-bắt-breaking-change-ở-package-khác">
<summary><strong>Gợi ý symbol thay đổi để bắt breaking change ở package khác</strong></summary>

Bot nhóm file review theo cùng thư mục (`groupByDirectory`), giải quyết tốt case impl + test cùng thư mục nhưng bỏ sót case 1 thay đổi ảnh hưởng file ở **package khác** (vd đổi signature 1 method nhưng nơi gọi nằm ở package khác — đặc biệt rủi ro với Go do interface ngầm định).

Để bù lại mà không cần parser AST đầy đủ: bot trích best-effort tên symbol (hàm/type/method) xuất hiện ở dòng thay đổi trong diff, liệt kê vào prompt kèm hướng dẫn Claude tự `grep`/tìm kiếm các tên đó ở nơi khác trong repo trước khi kết luận không có breaking change — thay vì chỉ dựa vào diff hoặc heuristic thư mục.

</details>

<details id="ngữ-cảnh-dùng-chung-giữa-các-phần-khi-pr-bị-chia-bundle">
<summary><strong>Ngữ cảnh dùng chung giữa các phần khi PR bị chia bundle</strong></summary>

Khi 1 PR lớn bị chia thành nhiều bundle (mỗi bundle là 1 lần gọi `claude -p` riêng), bot tổng hợp sẵn 1 lần trước khi chia:

- Danh sách **toàn bộ** file bị đổi trong PR (không chỉ file của riêng từng bundle).
- Đường dẫn README/convention doc gần nhất tìm được trong repo (ở root và ở thư mục của từng file thay đổi).

Ngữ cảnh này được nhúng y hệt vào đầu prompt của **mọi** bundle, thay cho việc chỉ dặn chung chung "hãy tự đọc thêm file liên quan" và để mỗi bundle tự quyết định lại — tránh mỗi phần của cùng 1 PR tự khám phá lại từ đầu (tốn turn/token) hoặc bỏ qua luôn (review thiếu ngữ cảnh, không nhất quán giữa các phần). PR không bị chia bundle (đa số) không tốn công build phần này.

</details>

<details id="tự-động-đọc-gitignore-của-repo">
<summary><strong>Tự động đọc <code>.gitignore</code> của repo</strong></summary>

Ngoài `exclude` ở `.yuumi.yml`, bot còn tự đọc file `.gitignore` thật ở root repo được review và **gộp thêm** pattern trong đó vào danh sách loại trừ (cộng dồn với default + `.yuumi.yml`, không thay thế) — repo nào đã tự đánh dấu 1 thư mục/file là "không cần track" (`coverage/`, `.turbo/`, `*.log`...) thì bot cũng không review nhầm nó.

Chỉ hỗ trợ các case phổ biến nhất, không phải toàn bộ spec `.gitignore`: comment/dòng trống/pattern phủ định (`!...`) bị bỏ qua, pattern có `/` (thư mục hoặc path lồng nhau) và pattern basename/đuôi file cố định hoạt động bình thường, wildcard đơn giản dạng `*.ext` cũng dịch được — wildcard phức tạp hơn (`file?.txt`, `[a-z]*`...) bị bỏ qua (không cố dịch sai). Không có `.gitignore` hoặc đọc lỗi đều không chặn review.

</details>

<details id="review-lần-2-trở-đi-chỉ-xem-phần-thay-đổi-mới">
<summary><strong>Review lần 2 trở đi chỉ xem phần thay đổi mới</strong></summary>

Mỗi lần review xong, bot ghi lại SHA vừa review cho đúng PR đó (`internal/reviewstate`, mặc định `logs/review-state.json`, override qua `REVIEW_STATE_FILE`). Lần review kế tiếp trên **cùng PR** (vd tác giả push thêm commit rồi mention lại `@yuumi review`) sẽ tự lấy diff qua GitHub compare API (`GET /compare/{sha_cũ}...{sha_mới}`) — chỉ chứa phần thay đổi MỚI — thay vì gửi lại toàn bộ diff so với base như trước, giúp tiết kiệm token/thời gian gọi Claude CLI đáng kể trên PR có nhiều vòng review.

- Lần đầu review 1 PR (chưa có state) vẫn hoạt động như cũ: lấy full diff so với base.
- Lấy state hoặc gọi compare API lỗi đều fallback về full diff, không chặn review.
- Review lỗi (Claude CLI lỗi, ...) thì SHA đó **không** được ghi nhận là đã review — lần sau vẫn tính từ SHA đã review thành công gần nhất, tránh bỏ sót phần code chưa thực sự được xem qua.
- Comment sẽ có ghi chú `_(Chỉ review phần thay đổi mới so với lần review trước...)_` để người đọc biết bot có tối ưu, không phải review sót.

</details>

<details id="kết-quả-review-có-phân-loại--comment-inline-theo-đúng-dòng-code">
<summary><strong>Kết quả review có phân loại + comment inline theo đúng dòng code</strong></summary>

Claude được yêu cầu trả kết quả dưới dạng JSON array các "finding" (`category`, `severity`, `message`, `suggestion`, kèm `file`/`line` nếu áp dụng được cho 1 dòng cụ thể) thay vì 1 khối text tự do.

- **Finding có `file`/`line` khớp đúng với diff thật** (đối chiếu lại bằng cách tự parse hunk của diff, không tin thẳng Claude) → post thành **comment inline** gắn đúng vào dòng đó qua GitHub Reviews API (`POST /pulls/{number}/reviews`), thay vì 1 dòng text lẫn trong comment tổng.
- **Finding không có `file`/`line`, hoặc có nhưng không khớp được với diff thật** (Claude "bịa" file/dòng không tồn tại, hoặc diễn giải lại thay vì copy nguyên văn số dòng) → vẫn hiển thị trong comment tổng hợp (placeholder), phân loại rõ theo severity (🔴 critical/🟠 high/🟡 medium/🔵 low) kèm category và gợi ý sửa nếu có — không bao giờ bị mất hay post sai chỗ trong im lặng.
- Claude trả text không đúng format JSON (bất chấp hướng dẫn) → fallback hiển thị nguyên văn như comment tổng hợp, review không bị coi là lỗi chỉ vì sai định dạng output.
- Post inline comment là bước **best-effort**, tách riêng khỏi comment tổng hợp: lỗi ở bước này (rate limit, lỗi mạng...) chỉ log lại, không làm mất kết quả review đã post thành công ở comment chính.

</details>

<details id="auto-review-khi-pr-mới-mở--có-commit-mới">
<summary><strong>Auto review khi PR mới mở / có commit mới</strong></summary>

Ngoài mention thủ công, bot còn tự chạy review khi nhận webhook event `pull_request` với action `opened` (PR mới tạo) hoặc `synchronize` (có commit mới push lên PR) — không cần ai gõ `@yuumi review`.

- **Setup webhook trên GitHub**: ngoài event `Issue comments` đã cấu hình cho luồng mention, cần bật thêm event **`Pull requests`** (Settings → Webhooks → chọn repo → "Let me select individual events"). Server phân biệt 2 loại event qua header `X-GitHub-Event` (không dựa vào field `action` trong body, vì cả 2 event đều có field này nhưng ý nghĩa khác nhau).
- **Allowlist**: `ALLOWED_USERS` (biến môi trường vốn dùng để chặn ai được phép mention bot) được **tái dùng** cho auto-review — chỉ tự động review PR do chính tác giả (`pull_request.user.login`) nằm trong danh sách này tạo ra, để không tự ý review "miễn phí" mọi PR của bất kỳ ai gửi vào repo đã cài webhook.
- **Dùng chung logic review** với luồng mention: cả 2 luồng cùng dựng `review.Job` như nhau (chỉ khác cách lấy `RepoFullName`/`IssueNumber`/comment đầu vào), và cùng dùng cơ chế tra cứu "SHA đã review lần trước" (`review.AlreadyReviewedSHA`) để tránh review trùng: PR đã được auto-review lúc mở, sau đó có người mention `@yuumi review` lại đúng SHA đó (hoặc GitHub redeliver webhook trùng) sẽ bị bỏ qua thay vì tốn thêm 1 lần gọi Claude CLI cho việc không có gì mới.
- Action khác `opened`/`synchronize` của event `pull_request` (`closed`, `reopened`, `edited`, `labeled`...) không kích hoạt gì cả.

</details>

## Eval suite

`evalsuite/` chứa các fixture PR có bug cài sẵn (`sql-injection-go`, `go-goroutine-leak`, `js-floating-promise`, `python-mutable-default`) để trả lời câu hỏi "đổi prompt/model có làm review tốt hơn hay tệ đi?":

```bash
go run ./cmd/evalrun                    # chạy toàn bộ fixture
go run ./cmd/evalrun sql-injection-go   # chỉ 1 fixture
```

Cần `claude` CLI đã authenticate. Đây là công cụ chạy tay, **không** nằm trong `go test`/CI. Đối chiếu output với `expected.md` của từng fixture rồi ghi 1 dòng vào `evalsuite/results.md`. Chi tiết và cách thêm fixture: [evalsuite/README.md](./evalsuite/README.md).

## Roadmap

**Đã fix limitation cũ:** trước đây clone `--depth 1` nên Claude không `git diff` được, chỉ đoán qua commit message. Giờ diff thật lấy trực tiếp từ GitHub API (không phụ thuộc git history), nên vẫn giữ `--depth 1` khi clone bình thường — nếu gọi GitHub API lỗi thì fallback về cách cũ (đọc file + commit message).

- [ ] `gitrepo.CloneRepo` cần nhúng token vào URL khi fetch nếu sau này review repo private (hiện chỉ work với repo public) — issue #48
- [ ] Deploy có URL public thật (thay vì chỉ test local qua curl/ngrok) — issue #46
- [ ] Đóng gói Docker — issue #49
- [ ] Deploy AWS — issue #50

## Đóng góp

Xem [CONTRIBUTING.md](./CONTRIBUTING.md) để biết cách chạy local, chạy unit test/CI, và test webhook thật (curl + ngrok).

## License

[MIT](./LICENSE)
