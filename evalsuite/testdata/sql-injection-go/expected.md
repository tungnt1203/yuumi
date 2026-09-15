# Kỳ vọng: sql-injection-go

PR thêm hàm `FindUsersBySearchTerm` build câu SQL bằng `fmt.Sprintf` nối
thẳng `term` (input từ ô search) vào query, thay vì dùng placeholder `?`
như 2 hàm còn lại trong cùng file — SQL injection kinh điển.

Bot PHẢI bắt được:

- [ ] Có finding `category: "security"` (hoặc `"bug"`, chấp nhận cả 2 nếu
      Claude phân loại khác) nhắc tới SQL injection / nối string vào query.
- [ ] Finding gắn đúng vào file `query.go`, dòng chứa `fmt.Sprintf(...)`
      hoặc dòng `db.Query(query)`.
- [ ] `suggestion` (nếu có) gợi ý dùng placeholder/parameterized query,
      không tự ý "sửa" bằng cách escape string thủ công.

Không nên báo sai (false positive) ở `FindUserByName`/`FindUsersByRole` —
2 hàm này đã dùng placeholder đúng cách, không đổi gì trong PR này.
