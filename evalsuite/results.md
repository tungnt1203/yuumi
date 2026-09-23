# Kết quả chạy eval suite theo thời gian

1 dòng / lần chạy `go run ./cmd/evalrun` — ghi lại NGAY SAU khi đối chiếu
`Response` với `expected.md` của từng fixture, để so sánh được qua các lần
đổi prompt/model sau này (xem [README.md](./README.md)).

`Bắt được?`: ✅ đúng như expected.md · ⚠️ bắt được nhưng thiếu chi tiết
(sai dòng, category khác...) · ❌ không bắt được / báo sai.

| Ngày | Commit/thay đổi | Fixture | Bắt được? | Ghi chú |
|------|-----------------|---------|-----------|---------|
| 2026-09-15 | baseline (khi thêm eval suite, issue #10) | sql-injection-go | ✅ | category=security, đúng dòng, suggestion dùng placeholder đúng convention file |
| 2026-09-15 | baseline (khi thêm eval suite, issue #10) | python-mutable-default | ✅ | Bắt đúng bug chính + thêm 2 finding phụ hợp lý (mutate list của caller, docstring sai) |
| 2026-09-15 | baseline (khi thêm eval suite, issue #10) | go-goroutine-leak | ✅ | Bắt đúng bug chính (leak trong Notify) + vài finding phụ hợp lý (println debug, channel không buffer) |
| 2026-09-15 | baseline (khi thêm eval suite, issue #10) | js-floating-promise | ✅ | Bắt đúng floating promise, đúng dòng, suggestion dùng .catch(...) đúng như expected |
| 2026-09-23 | rule secret + regex pre-scan (issue #64) | hardcoded-secret-go | ✅ | category=security, severity=high (Claude nhận ra key là ví dụ AWS EXAMPLE nên hạ từ critical), đúng dòng 21-22, suggestion dùng os.Getenv; thêm 1 finding phụ hợp lý (validate cặp env var) |
