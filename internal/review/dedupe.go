package review

// AlreadyReviewedSHA báo PR (repoFullName, issueNumber) đã được review xong
// tới đúng sha này chưa, dựa trên state đã lưu (xem ReviewStateStore, issue
// #21). Dùng CHUNG cho cả 2 luồng trigger review (mention thủ công lẫn
// auto-review khi PR mở/có commit mới, issue #32) để chia sẻ đúng 1 cơ chế
// tránh review trùng, thay vì mỗi luồng tự xây dedup riêng: PR đã được
// auto-review lúc mở/push, sau đó có người mention lại đúng SHA đó thì
// không nên tốn thêm 1 lần gọi Claude CLI chỉ để nhận ra không có gì mới.
//
// store == nil (StateStore chưa cấu hình) hoặc tra cứu lỗi đều trả về false
// — "chưa chắc đã review" là lựa chọn an toàn hơn "coi như đã review" khi
// không đủ thông tin để khẳng định, review vẫn nên chạy thay vì bị bỏ sót.
func AlreadyReviewedSHA(store ReviewStateStore, repoFullName string, issueNumber int, sha string) bool {
	if store == nil || sha == "" {
		return false
	}
	lastSHA, found, err := store.LastReviewedSHA(repoFullName, issueNumber)
	if err != nil || !found {
		return false
	}
	return lastSHA == sha
}
