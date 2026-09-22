# Contributing

Hướng dẫn dành cho người muốn chạy/sửa code local, test webhook thật, hoặc
đóng góp PR.

## Chạy local

```bash
set -a && source .env && set +a
go run ./cmd/server
```

Server lắng nghe cổng `:8080`, có 2 route:

- `GET /health` — trả trạng thái thật của các dependency (check lúc khởi động, cache lại, không gọi CLI/API mỗi request): `200` kèm JSON `{"claude_cli":{"ok":true,...},"github_app":{"ok":true,...},"checked_at":"..."}` nếu mọi thứ OK, `503` nếu có dependency lỗi.
- `POST /webhook` — endpoint nhận GitHub webhook (event `issue_comment` cho mention thủ công, `pull_request` cho auto-review — phân biệt qua header `X-GitHub-Event`, xem [README](./README.md#auto-review-khi-pr-mới-mở--có-commit-mới)).

**Lưu ý:** `issue.number` trong payload phải là số của 1 **Pull Request thật** (không phải Issue thường), vì bước lấy head SHA gọi API `/pulls/{number}` — trên Issue thường API này trả 404.

## Unit test và CI

```bash
go build ./... && go vet ./... && go test ./...
```

3 lệnh này cũng là những gì CI (`.github/workflows/ci.yml`) chạy trên mỗi PR và mỗi push vào `main`. Unit test không cần token hay `claude` CLI.

## Test thủ công (giả lập webhook GitHub)

**Lưu ý (issue #47):** `<installation id>` phải là ID **thật** của lần cài App vào 1 repo — server sẽ gọi GitHub thật để đổi lấy installation token trước khi làm gì khác, không "giả lập hoàn toàn offline" được. Lấy ID này ở App settings → **Advanced** → chọn 1 delivery bất kỳ → xem field `installation.id`, hoặc từ URL trang cài đặt của installation (`.../installations/<id>`).

```bash
BODY='{"action":"created","comment":{"id":1,"body":"@yuumi review","user":{"login":"<username>"}},"repository":{"full_name":"<owner>/<repo>"},"issue":{"number":<số PR>},"installation":{"id":<installation id thật>}}'
SIG=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" | sed 's/^.* //')
curl -i -X POST localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: issue_comment" \
  -H "X-Hub-Signature-256: sha256=$SIG" \
  -d "$BODY"
```

(`comment.id` là giả nên bước react 👀 sẽ luôn báo lỗi 404 — bình thường, không chặn các bước sau)

Giả lập auto-review (event `pull_request` — `<username>` phải nằm trong `ALLOWED_USERS`):

```bash
BODY='{"action":"opened","repository":{"full_name":"<owner>/<repo>"},"pull_request":{"number":<số PR>,"head":{"sha":"<head sha>"},"user":{"login":"<username>"}},"installation":{"id":<installation id thật>}}'
SIG=$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" | sed 's/^.* //')
curl -i -X POST localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: pull_request" \
  -H "X-Hub-Signature-256: sha256=$SIG" \
  -d "$BODY"
```

## Test thật với GitHub qua ngrok

Để thử với webhook GitHub thật khi chưa deploy, mở tunnel từ máy local ra URL public bằng [ngrok](https://ngrok.com):

1. Chạy server: `set -a && source .env && set +a && go run ./cmd/server`, rồi kiểm tra `curl -i localhost:8080/health` trả `200`.
2. Mở tunnel ở terminal khác: `ngrok http 8080` (hoặc `ngrok http --url=<domain-cố-định> 8080` nếu có domain ngrok cố định, để không phải sửa lại webhook mỗi lần chạy lại). Có thể xem từng request GitHub gửi tới ở `http://127.0.0.1:4040`.
3. Với GitHub App, webhook chỉ cấu hình **1 lần trên chính App** (không phải trên từng repo). Vào App settings (**Settings → Developer settings → GitHub Apps → \<tên App\>**):
   - **Webhook URL**: `https://<domain-ngrok>/webhook` (viết liền, không có khoảng trắng, phải có `/webhook`) — sửa lại mỗi khi domain ngrok đổi.
   - **Webhook secret**: đúng giá trị `GITHUB_WEBHOOK_SECRET` trong `.env` (sai secret server trả `401`).
   - **Permissions & events**: `Issues: Read and write`, `Pull requests: Read and write`, subscribe **Issue comment** + **Pull request**.
   - Nếu App chưa cài vào repo đích: **Install App** (menu bên trái) → chọn repo.
4. Tab **Advanced** của App: xem **Recent Deliveries**, event `ping` đầu tiên phải có dấu tick xanh. Log server sẽ in `Ignored: unsupported X-GitHub-Event ping` — bình thường, server chỉ xử lý `issue_comment` và `pull_request`.
5. Comment `@yuumi review` trên 1 **Pull Request thật** bằng tài khoản có trong `ALLOWED_USERS`, hoặc mở PR mới / push thêm commit để thử auto-review (tác giả PR phải nằm trong `ALLOWED_USERS`). Bot sẽ react 👀, hiện "Đang review...", rồi sửa comment đó thành kết quả review — dưới tên **`<tên App>[bot]`** thay vì tài khoản cá nhân.

## Eval suite

Xem [evalsuite/README.md](./evalsuite/README.md) để biết cách chạy và thêm fixture đo chất lượng review.

## Thêm fixture / rule mới

- Rule mặc định theo loại file nằm ở `internal/review` — xem [README](./README.md#rule-mặc-định-theo-loại-file) để biết các rule hiện có trước khi thêm rule mới, tránh trùng.
- Luôn chạy `go build ./... && go vet ./... && go test ./...` trước khi mở PR.
