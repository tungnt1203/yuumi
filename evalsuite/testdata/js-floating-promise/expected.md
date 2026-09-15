# Kỳ vọng: js-floating-promise

PR thêm `logAudit(user.id, "viewed")` trong `handleRequest` — gọi 1 hàm
`async` nhưng KHÔNG `await` và KHÔNG `.catch(...)`: promise "bay" tự do
(floating promise). Nếu `logAudit` reject (vd API audit log lỗi/timeout),
lỗi biến mất im lặng (unhandled rejection), không ai biết audit log có ghi
được hay không.

Bot PHẢI bắt được:

- [ ] Có finding nhắc tới floating promise / thiếu `await`/`.catch` ở dòng
      gọi `logAudit(user.id, "viewed")`.
- [ ] Finding gắn đúng vào file `handler.js`, đúng dòng gọi `logAudit(...)`
      trong `handleRequest`.
- [ ] `suggestion` (nếu có) gợi ý `await logAudit(...)` (nếu chấp nhận
      chặn response) hoặc `logAudit(...).catch(...)` (nếu cố tình
      "fire-and-forget" nhưng cần bắt lỗi).

Không nên báo sai ở `fetchUser` — hàm này đã `await` đầy đủ, không đổi gì
trong PR này.
