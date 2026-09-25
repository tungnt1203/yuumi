# Kỳ vọng: cross-package-nil-go

Fixture cho issue #73: lỗi chỉ thấy được khi nhìn cả 2 package cùng lúc.
Diff ~17k ký tự, với ngân sách mặc định 12k thì `api/` và `store/` rơi vào
2 bundle khác nhau (git xếp file theo tên: `api` đầu, `store` cuối).

- `store/user.go`: `Get(id) (User, error)` đổi thành
  `Get(ctx, id) (*User, error)`, và **không tìm thấy trả `(nil, nil)`**
  (doc comment ghi rõ, `ErrNotFound` bị xoá). Riêng thay đổi này hợp lệ.
- `api/handler.go`: `GetUser` gọi `Get` mới, bỏ nhánh 404 cũ và dùng
  `u.ID`/`u.Name`/`u.Email` mà không kiểm tra `u == nil` → **nil pointer
  dereference (panic) khi id không tồn tại**, đồng thời mất response 404.
  Nhìn riêng diff của `api/` thì không thấy lỗi.

Bot PHẢI bắt được:

- [ ] Có finding `category: "bug"` nói `GetUser` dereference `u` khi `Get`
      trả `(nil, nil)` (user không tồn tại) → panic / mất 404.
- [ ] Finding gắn vào `api/handler.go`, dòng `writeJSON(w, userResponse{...})`
      trong `GetUser` (hoặc dòng gọi `h.users.Get`). Finding chỉ ở
      `store/user.go` kiểu "trả nil, nil nguy hiểm cho caller" mà không chỉ
      ra `GetUser` thì tính ⚠️.

Không nên báo `bug`/`security` mức medium trở lên ở `billing/`, `notify/`,
`report/` — 3 package mới này không có lỗi được cấy. Góp ý mức low về
validate đầu vào/tràn số là chấp nhận được, và `safeCell` thiếu `\t`/`\r`
theo khuyến nghị OWASP là góp ý đúng. Doc comment của `store.Get` viện dẫn
"driver Postgres trả (nil, nil)" là sai thật (database/sql, pgx đều trả
ErrNoRows) — bot chỉ ra được thì là điểm cộng.

Khi so ngân sách (`go run ./cmd/evalrun -budget N cross-package-nil-go`),
ghi cả số bundle và tổng chi phí vào results.md.
