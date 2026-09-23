package review

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// secretRules là rule "luôn chèn" nhắc Claude soát secret/credential
// hardcode (issue #64). Khác defaultLanguageRules (langrules.go) — chỉ chèn
// theo đuôi file — secret có thể nằm ở BẤT KỲ loại file nào (code, YAML,
// .env, Dockerfile, README...) nên rule này không phụ thuộc extension, và
// là loại lỗi hậu quả nặng nhất nếu bị bỏ sót (key lộ lên git history là
// coi như đã lộ, xoá commit sau cũng không thu hồi được).
const secretRules = "- Secret/credential hardcode: API key, password, token, private key, connection string có kèm user/password, webhook URL có token... được gán thẳng giá trị thật trong code/config thay vì đọc từ biến môi trường hoặc secret manager.\n" +
	"- Pattern thường gặp: AWS access key (AKIA...), \"-----BEGIN ... PRIVATE KEY-----\", token dạng sk-..., ghp_..., xoxb-..., password = \"...\", postgres://user:pass@host.\n" +
	"- Nếu là secret thật (không phải giá trị giả rõ ràng như \"changeme\", \"xxx\", placeholder trong test/ví dụ) → finding category \"security\", severity \"critical\" (hoặc \"high\" nếu chỉ là môi trường dev/test); suggestion đọc từ biến môi trường/secret manager. KHÔNG chép lại giá trị secret vào message/suggestion."

// secretPattern là 1 dạng secret đủ đặc trưng để bắt bằng regex mà ít báo
// sai — lớp quét nhanh chạy trước Claude, cùng tinh thần staticcheck.go
// (máy bắt được chỗ rõ ràng thì Claude khỏi phải tự tìm ra), còn case tinh
// vi hơn (credential lồng trong biến, ghép chuỗi...) vẫn để Claude xử lý
// qua secretRules.
type secretPattern struct {
	label string
	re    *regexp.Regexp
	// skip (nếu có) khớp dòng thì bỏ qua dù re khớp — loại false positive
	// đã biết trước của pattern đó.
	skip *regexp.Regexp
	// configOnly: chỉ áp dụng cho file config/.env (xem isConfigFile) —
	// pattern quá rộng nếu chạy trên code (vd "password = cfg.Password").
	configOnly bool
}

var secretPatterns = []secretPattern{
	{label: "AWS access key", re: regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{label: "private key (PEM)", re: regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----`)},
	{label: "GitHub token", re: regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`)},
	{label: "API key dạng sk-...", re: regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{label: "Slack token", re: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{label: "connection string có password", re: regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@"']+:[^\s@/"']+@`)},
	// Tên biến kiểu password/secret/token gán bằng 1 string literal. Yêu cầu
	// literal ≥ 6 ký tự để bỏ qua giá trị rỗng/quá ngắn kiểu "" hay "x".
	// skip: giá trị chỉ là TÊN env var/header (const passwordEnv =
	// "DB_PASSWORD", apiKeyHeader = "X-Api-Key") — idiom Go rất phổ biến,
	// không phải secret.
	{
		label: "password/secret gán string literal",
		re:    regexp.MustCompile(`(?i)\b[a-z0-9_]*(?:password|passwd|secret|api_?key|access_?token|auth_?token)[a-z0-9_]*["']?\s*(?::=|=|:)\s*["'][^"'\s]{6,}["']`),
		skip:  regexp.MustCompile(`[:=]\s*["'](?:[A-Z][A-Z0-9_]{5,}|[A-Z][a-z]*(?:-[A-Z][a-z]*)+)["']`),
	},
	// Dạng không quote chiếm cả dòng — phổ biến nhất trong .env/YAML/
	// .properties (DB_PASSWORD=hunter22, password: hunter22). Bỏ qua giá
	// trị tham chiếu ($VAR, ${VAR}, <placeholder>).
	{
		label:      "password/secret gán giá trị không quote",
		re:         regexp.MustCompile(`(?i)^\s*(?:export\s+)?[a-z0-9_.-]*(?:password|passwd|secret|api_?key|access_?token|auth_?token)[a-z0-9_.-]*\s*[:=]\s*[^\s"'$\{<]{6,}\s*$`),
		configOnly: true,
	},
}

// configFileExts là đuôi file config dạng key=value/key: value, nơi secret
// hay bị ghi thẳng không quote.
var configFileExts = []string{".env", ".yml", ".yaml", ".properties", ".ini", ".conf", ".cfg", ".toml"}

// isConfigFile báo p có phải file config không — gồm cả biến thể .env.*
// (.env.local, .env.production) mà suffix-match không bắt được.
func isConfigFile(p string) bool {
	return strings.HasPrefix(path.Base(p), ".env") || matchesAnyPattern(p, configFileExts)
}

// maxSecretHits giới hạn số dòng liệt kê trong prompt — PR commit nhầm cả
// 1 file dump credential không nên làm phình prompt; vài chỗ đầu đủ để
// Claude biết cần soát kỹ, phần còn lại ghi tổng số.
const maxSecretHits = 20

// secretHit là 1 dòng THÊM MỚI trong diff khớp 1 secretPattern.
type secretHit struct {
	file  string
	line  int
	label string
}

// scanSecrets quét các dòng thêm mới (LineAdded) trong diff theo
// secretPatterns. Chỉ dòng thêm mới: secret đã có sẵn từ trước (dòng
// context) hay vừa bị xoá không phải trách nhiệm của PR này. Mỗi dòng chỉ
// ghi nhận pattern khớp đầu tiên — 1 dòng không cần báo 2 lần.
func scanSecrets(diff string) []secretHit {
	var hits []secretHit
	for _, fileDiff := range splitDiffByFile(diff) {
		fd := parseFileHunks(fileDiff)
		isConfig := isConfigFile(fd.NewPath)
		for _, h := range fd.Hunks {
			for _, l := range h.Lines {
				if l.Kind != LineAdded {
					continue
				}
				for _, p := range secretPatterns {
					if p.configOnly && !isConfig {
						continue
					}
					if p.skip != nil && p.skip.MatchString(l.Content) {
						continue
					}
					if p.re.MatchString(l.Content) {
						hits = append(hits, secretHit{file: fd.NewPath, line: l.NewLine, label: p.label})
						break
					}
				}
			}
		}
	}
	return hits
}

// secretScanNote render kết quả scanSecrets thành phần chèn vào prompt,
// "" nếu không có dòng nào khớp. Chỉ ghi file:dòng + loại, KHÔNG chép giá
// trị secret — prompt có thể bị log lại, không nên nhân bản secret thêm chỗ
// nào nữa (giá trị đã có sẵn trong khối diff cho Claude đọc).
func secretScanNote(diff string) string {
	hits := scanSecrets(diff)
	if len(hits) == 0 {
		return ""
	}

	shown := hits
	if len(shown) > maxSecretHits {
		shown = shown[:maxSecretHits]
	}

	var b strings.Builder
	b.WriteString("Quét regex tự động phát hiện dòng THÊM MỚI có dấu hiệu secret/credential hardcode (có thể là false positive — giá trị giả trong test/ví dụ; hãy tự xác minh từng chỗ, secret thật thì báo finding theo rule secret ở trên):\n")
	for _, h := range shown {
		fmt.Fprintf(&b, "- %s:%d — %s\n", h.file, h.line, h.label)
	}
	if extra := len(hits) - len(shown); extra > 0 {
		fmt.Fprintf(&b, "- (và %d dòng khác)\n", extra)
	}
	return strings.TrimRight(b.String(), "\n")
}
