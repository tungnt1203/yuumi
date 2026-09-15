# Kỳ vọng: python-mutable-default

PR thêm `apply_discounts(cart, discounts=[])` — default argument là 1 LIST
(mutable), và hàm còn `discounts.append("applied")` ngay trên chính object
default đó nếu caller không truyền `discounts` riêng — giá trị mặc định bị
CHIA SẺ và tích luỹ dữ liệu giữa các lần gọi khác nhau (lỗi kinh điển của
Python), khác hẳn cách `Cart.__init__` xử lý đúng ngay phía trên (dùng
`None` rồi tạo list mới bên trong).

Bot PHẢI bắt được:

- [ ] Có finding nhắc tới mutable default argument ở hàm `apply_discounts`.
- [ ] Finding gắn đúng vào file `cart.py`, dòng `def apply_discounts(cart, discounts=[]):`.
- [ ] `suggestion` (nếu có) gợi ý theo đúng pattern `Cart.__init__` ngay
      trong file này: `discounts=None` rồi `if discounts is None: discounts = []`.

Không nên báo sai ở `Cart.__init__`/`add` — không đổi gì trong PR này và đã
xử lý default đúng cách.
