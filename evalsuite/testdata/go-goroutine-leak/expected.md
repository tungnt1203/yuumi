# Kỳ vọng: go-goroutine-leak

PR thêm `Notify` — mỗi lần gọi spawn 1 goroutine mới `for { msg := <-p.events; ... }`
chạy MÃI MÃI, không có cách nào dừng (không nhận `context`, không có
channel "done", không `return` sau khi xử lý). Gọi `Notify` nhiều lần =
leak thêm goroutine mỗi lần, tất cả đều chờ đọc chung 1 channel không bao
giờ đóng.

Bot PHẢI bắt được:

- [ ] Có finding nhắc tới goroutine leak / goroutine không có cách dừng
      (context/channel cancel) trong hàm `Notify`.
- [ ] Finding gắn đúng vào file `worker.go`, khoảng dòng `go func() { for { ... } }()`
      trong `Notify`.

Không nên báo sai ở `Start` — goroutine đó đã có `ctx.Done()` để dừng đúng
cách, không đổi gì trong PR này.
