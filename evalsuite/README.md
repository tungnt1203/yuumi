# Eval suite — đánh giá chất lượng review theo thời gian (issue #10)

Bộ fixture để trả lời câu hỏi "đổi prompt/model có làm review tốt hơn hay
tệ đi?" — trước giờ chỉ test thủ công qua vài PR thật, không có gì để so
sánh lại khi đổi `internal/review/prompt.go` hay cấu hình `claude` CLI.

## Cách hoạt động

Mỗi fixture ở `testdata/<tên>/` mô phỏng 1 PR có bug đã biết trước, cấy chủ
đích:

```
testdata/<tên>/
  before/        # state code TRƯỚC PR (base)
  after/         # state code SAU PR (head) — bug được cấy ở đây
  expected.md    # checklist bug bot PHẢI bắt được
```

`go run ./cmd/evalrun` tự dựng 1 git repo tạm để lấy diff thật giữa
`before/` và `after/` (đúng shape unified diff GitHub trả về), gọi thẳng
`review.BuildReviewPrompt` + `claudecli.Reviewer` — bỏ qua webhook/GitHub
API vì mục đích ở đây là đánh giá CHẤT LƯỢNG prompt/model, không phải test
lại luồng nhận webhook (đã có `internal/review` unit test riêng cho việc
đó).

```bash
go run ./cmd/evalrun                          # chạy toàn bộ fixture
go run ./cmd/evalrun sql-injection-go         # chỉ 1 fixture
```

Cần `claude` CLI đã cài + authenticate (giống yêu cầu chạy server thật, xem
README gốc) — đây KHÔNG phải lệnh chạy trong `go test`/CI, mà là công cụ
chạy tay khi cần đánh giá 1 thay đổi.

## Quy trình đánh giá (thủ công — giai đoạn đầu, issue #10 cho phép)

1. Chạy `go run ./cmd/evalrun` trước khi đổi (baseline) và sau khi đổi
   prompt/model.
2. Với mỗi fixture, đọc phần `Response` in ra, tự đối chiếu với checklist
   `expected.md` — bot có nhắc đúng vấn đề, đúng file, đúng dòng không.
3. Ghi lại kết quả vào [`results.md`](./results.md) (1 dòng/lần chạy) để so
   sánh được qua thời gian, không chỉ nhớ trong đầu.

Tự động hoá việc đối chiếu (assert `category`/keyword có mặt trong JSON
response) là bước SAU, một khi có đủ fixture để việc đó đáng công — hiện
tại checklist tay là đủ theo đúng đề xuất của issue #10.

## Thêm fixture mới

1. Tạo `testdata/<tên-ngắn-gọn>/before/` + `after/` — 1-2 file nhỏ là đủ,
   chỉ cấy ĐÚNG 1 bug rõ ràng để dễ đối chiếu (đừng gộp nhiều bug không
   liên quan vào cùng 1 fixture).
2. Viết `expected.md`: mô tả bug, checklist bot phải bắt được (category,
   file/dòng, có báo sai (false positive) ở phần code không đổi không).
3. Chạy thử `go run ./cmd/evalrun <tên-fixture>` để chắc diff dựng đúng và
   review chạy được trước khi coi là fixture hợp lệ.

Ưu tiên phủ đúng các loại rule mặc định bot đã tự nhận có (xem README gốc,
mục "Rule mặc định theo loại file") — 4 fixture hiện có (`sql-injection-go`,
`go-goroutine-leak`, `js-floating-promise`, `python-mutable-default`) mỗi
cái tương ứng 1 rule, để biết rule đó thực sự "có tác dụng" hay chỉ nằm
trong prompt cho có.
