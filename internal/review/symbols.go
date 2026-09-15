package review

import (
	"fmt"
	"regexp"
	"strings"
)

// Cơ chế gom nhóm "file liên quan" duy nhất hiện có (groupByDirectory,
// diffsplit.go) dựa trên cùng thư mục cha trực tiếp — giải quyết tốt case
// impl + test cùng thư mục, nhưng bỏ sót hoàn toàn case 1 thay đổi ảnh
// hưởng file ở PACKAGE KHÁC (vd đổi signature 1 hàm/method nhưng nơi gọi nó
// nằm ở package khác không được đưa vào cùng ngữ cảnh) — đặc biệt rủi ro
// với Go do interface ngầm định (implicit interface): đổi method trong 1
// struct mà không có gì báo hiệu chỗ khác đang phụ thuộc vào nó qua
// interface. Xem issue #19.
//
// Không dùng parser AST đầy đủ (over-engineering cho quy mô hiện tại, đúng
// tinh thần issue #19 đề ra) — bước tối thiểu, thực dụng: trích best-effort
// tên symbol xuất hiện ở dòng THAY ĐỔI trong diff bằng regex đơn giản, dặn
// Claude tự grep/tìm kiếm các tên đó trong toàn repo trước khi kết luận,
// thay vì chỉ dựa vào diff/heuristic thư mục.

// identifierCallRe khớp "tên(" — bắt được phần lớn định nghĩa/gọi hàm hay
// method (kể cả method có receiver kiểu Go: "func (r *T) Name(args)" khớp
// đúng "Name("), bất kể ngôn ngữ (Go/JS/TS/Python — xem issue #25) vì cú
// pháp gọi/định nghĩa hàm gần như giống nhau ở mọi ngôn ngữ phổ biến bot
// review — không cần parser riêng cho từng ngôn ngữ.
var identifierCallRe = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

// goTypeDeclRe khớp khai báo type Go ("type Name struct"/"type Name
// interface") — identifierCallRe không bắt được case này vì không có dấu
// "(" ngay sau tên type. Đây đúng là rủi ro Go cụ thể issue #19 nêu ra
// (interface ngầm định), đáng bắt riêng thay vì bỏ sót.
var goTypeDeclRe = regexp.MustCompile(`\btype\s+([A-Za-z_][A-Za-z0-9_]*)\s+(?:struct|interface)\b`)

// commonKeywords là từ khoá phổ biến của nhiều ngôn ngữ hay bị
// identifierCallRe bắt nhầm thành "symbol" vì cũng có dạng "keyword(" (vd
// "if (x)", "for (i...)", "switch (x)") — loại trừ để danh sách symbol
// không bị rác những thứ không phải tên hàm/type/method thật.
var commonKeywords = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true,
	"func": true, "function": true, "def": true,
	"return": true, "catch": true, "else": true, "elif": true,
	"except": true, "with": true, "case": true,
}

// maxChangedSymbols giới hạn số symbol đưa vào prompt — PR lớn có thể tạo
// ra hàng trăm match, đưa hết vào chỉ tốn token vô ích (model không cần
// biết TẤT CẢ, chỉ cần đủ để tự grep những cái đáng ngờ nhất). Ưu tiên
// symbol xuất hiện SỚM trong diff (thường là symbol chính của thay đổi).
const maxChangedSymbols = 30

// extractChangedSymbols trích best-effort tên symbol (hàm/type/method) xuất
// hiện ở dòng THAY ĐỔI (+/-) trong diff, giữ đúng thứ tự xuất hiện đầu tiên,
// không trùng lặp. Không cần chính xác 100% — mục đích là gợi ý cho Claude
// tự kiểm tra, false positive/sót không sao (model tự grep sẽ tự lọc).
func extractChangedSymbols(diff string) []string {
	seen := make(map[string]bool)
	var symbols []string

	add := func(name string) {
		if name == "" || commonKeywords[name] || seen[name] {
			return
		}
		seen[name] = true
		symbols = append(symbols, name)
	}

	for _, line := range strings.Split(diff, "\n") {
		if len(line) < 2 {
			continue
		}
		// "+++ "/"--- " là header file diff (đường dẫn file), không phải
		// nội dung code — dù cũng bắt đầu bằng '+'/'-' phải loại trừ riêng.
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		prefix := line[0]
		if prefix != '+' && prefix != '-' {
			continue
		}
		content := line[1:]

		for _, m := range goTypeDeclRe.FindAllStringSubmatch(content, -1) {
			add(m[1])
		}
		for _, m := range identifierCallRe.FindAllStringSubmatch(content, -1) {
			add(m[1])
		}
	}

	return symbols
}

// changedSymbolsNote render phần prompt liệt kê symbol thay đổi + dặn Claude
// tự grep tìm nơi dùng trước khi kết luận không có breaking change (xem
// extractChangedSymbols, issue #19). Trả "" nếu không trích được symbol nào
// (diff không có gì giống code — vd chỉ sửa markdown/config) — không thêm
// section rỗng vô nghĩa vào prompt.
func changedSymbolsNote(diff string) string {
	symbols := extractChangedSymbols(diff)
	if len(symbols) == 0 {
		return ""
	}

	shown := symbols
	suffix := ""
	if len(shown) > maxChangedSymbols {
		shown = shown[:maxChangedSymbols]
		suffix = fmt.Sprintf(" (và %d symbol khác)", len(symbols)-maxChangedSymbols)
	}

	return fmt.Sprintf(
		"Các symbol (tên hàm/type/method) xuất hiện ở dòng thay đổi trong diff (trích tự động, có thể sót hoặc lẫn false positive): %s%s.\n"+
			"Trước khi kết luận không có breaking change, hãy tự grep/tìm kiếm các tên này ở NƠI KHÁC trong repo (không chỉ trong diff bên dưới) — đổi signature/behavior của 1 symbol có thể ảnh hưởng code gọi nó ở package khác mà bundle này không hiển thị, đặc biệt rủi ro với Go do interface ngầm định (implicit interface).",
		strings.Join(shown, ", "), suffix,
	)
}
