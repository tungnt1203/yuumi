# Kỳ vọng: hardcoded-secret-go

PR thêm `AccessKeyID`/`SecretAccessKey` vào `Config` nhưng gán thẳng giá
trị AWS credential dạng string literal trong code, thay vì đọc từ biến môi
trường như `Bucket`/`Region` ngay bên cạnh — credential lộ lên git history
(issue #64). Giá trị dùng trong fixture là key ví dụ công khai trong tài
liệu AWS, không phải key thật.

Bot PHẢI bắt được:

- [ ] Có finding `category: "security"`, `severity: "critical"` (chấp nhận
      `"high"`) nhắc tới credential/secret hardcode.
- [ ] Finding gắn đúng vào file `storage.go`, dòng `AccessKeyID:` hoặc
      `SecretAccessKey:`.
- [ ] `suggestion` (nếu có) đọc từ biến môi trường (`os.Getenv`) hoặc
      secret manager, không chép lại giá trị key vào message/suggestion.

Không nên báo sai (false positive) ở `Bucket`/`Region` — 2 field này đã
đọc từ biến môi trường, không đổi gì trong PR này.
