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
| 2026-09-25 | ngân sách bundle 12k (mặc định, issue #73) | cross-package-nil-go | ✅ | 2 bundle, $0.46. Bundle 1 (có api/) tự đọc store/user.go nhờ primer → bắt đúng nil deref ở api/handler.go:31; bundle 2 báo lại cùng lỗi từ phía store (trùng finding). Thêm: comment "driver Postgres trả nil,nil" sai (đúng), safeCell thiếu \t/\r (đúng), 2 góp ý low ở billing |
| 2026-09-25 | ngân sách bundle 100k (issue #73) | cross-package-nil-go | ✅ | 1 bundle, $0.30 (rẻ hơn 35%). Bắt đúng nil deref ở api/handler.go:30 (critical) + breaking change ở store, không trùng finding. Góp ý phụ ở billing/report tương tự lần 12k |
| 2026-09-25 | ngân sách bundle 12k, lần 2 | cross-package-nil-go | ✅ | 2 bundle, $0.36. Bắt nil deref ở api/handler.go:30 (high) và báo lại từ phía store (critical) — trùng finding. Thêm: Get vẫn trả user Disabled (đúng) |
| 2026-09-25 | ngân sách bundle 12k, lần 3 | cross-package-nil-go | ✅ | 2 bundle, $0.39. Bắt ở api/handler.go:31 (critical), trùng lại ở store/user.go:50 (high) |
| 2026-09-25 | ngân sách bundle 100k, lần 2 | cross-package-nil-go | ✅ | 1 bundle, $0.26. api/handler.go:30 (critical), không trùng |
| 2026-09-25 | ngân sách bundle 100k, lần 3 | cross-package-nil-go | ✅ | 1 bundle, $0.24. api/handler.go:30 (critical), không trùng. Thêm: Mailer.from không được dùng (đúng — lỗi thật trong fixture) |
| 2026-09-25 | cờ CLI #100 (--tools Read,Grep,Glob, --restricted, --no-session-persistence, --max-budget-usd) | cross-package-nil-go | ✅ | $0.22 (trước ~$0.27). api/handler.go:30 high, không trùng; thêm Mailer.from không dùng (đúng) |
| 2026-09-25 | cờ CLI #100 | go-goroutine-leak | ✅ | $0.09 (trước $0.19: bớt tool → system prompt ngắn, cache write 19.5k → 8.2k token). Leak trong Notify đúng dòng |
| 2026-09-25 | cờ CLI #100 | hardcoded-secret-go | ✅ | $0.09. security/high, đúng dòng 21-22, nhận ra key AWS EXAMPLE, suggestion os.Getenv |
| 2026-09-25 | cờ CLI #100 | js-floating-promise | ✅ | $0.08. Floating promise đúng dòng 20, suggestion .catch(...) |
| 2026-09-25 | cờ CLI #100 | python-mutable-default | ✅ | $0.09. Bắt đúng mutable default + mutate list của caller |
| 2026-09-25 | cờ CLI #100 | sql-injection-go | ✅ | $0.08. security/critical đúng dòng fmt.Sprintf |
