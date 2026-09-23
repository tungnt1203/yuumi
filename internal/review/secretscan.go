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
	// assignment: re có 2 group (tên key, giá trị) — match mà key/giá trị
	// chỉ là tham chiếu (xem isReferenceAssignment) bị bỏ qua. Xét TỪNG
	// match chứ không cả dòng: 1 literal tham chiếu trên cùng dòng không
	// được che mất secret thật đứng cạnh nó — lớp quét này thà báo thừa
	// (Claude xác minh lại) còn hơn bỏ sót.
	assignment bool
	// configOnly: chỉ áp dụng cho file config/.env (xem isConfigFile) —
	// pattern quá rộng nếu chạy trên code (vd "password = cfg.Password").
	configOnly bool
}

// secretKeyNames là phần tên key/biến gợi ý giá trị là secret.
const secretKeyNames = `(?:password|passwd|passphrase|secret|api_?key|access_?token|auth_?token)`

var secretPatterns = []secretPattern{
	{label: "AWS access key", re: regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{label: "private key (PEM)", re: regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----`)},
	{label: "GitHub token", re: regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`)},
	{label: "API key dạng sk-...", re: regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{label: "Slack token", re: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{label: "connection string có password", re: regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@"']+:[^\s@/"']+@`)},
	// Tên biến kiểu password/secret/token gán bằng 1 string literal. Yêu cầu
	// literal ≥ 6 ký tự để bỏ qua giá trị rỗng/quá ngắn kiểu "" hay "x".
	{
		label:      "password/secret gán string literal",
		re:         regexp.MustCompile(`(?i)\b([a-z0-9_]*` + secretKeyNames + `[a-z0-9_]*)["']?\s*(?::=|=|:)\s*["']([^"'\s]{6,})["']`),
		assignment: true,
	},
	// Dạng không quote chiếm cả dòng — phổ biến nhất trong .env/YAML/
	// .properties (DB_PASSWORD=hunter22, password: hunter22, item list
	// "- POSTGRES_PASSWORD=..." của docker-compose/k8s), cho phép comment
	// " # ..." cuối dòng. Như dotenv/YAML, "#" chỉ mở comment khi có khoảng
	// trắng đứng trước — "#" nằm trong giá trị (p#ssw0rd) vẫn là giá trị.
	// Bỏ qua giá trị tham chiếu ($VAR, ${VAR}, <placeholder>).
	{
		label:      "password/secret gán giá trị không quote",
		re:         regexp.MustCompile(`(?i)^\s*(?:-\s+)?(?:export\s+)?([a-z0-9_.-]*` + secretKeyNames + `[a-z0-9_.-]*)\s*[:=]\s*([^\s"'$\{<#][^\s"']{5,})(?:\s+#.*)?\s*$`),
		assignment: true,
		configOnly: true,
	},
}

// referenceKey: key mà giá trị là TÊN/đường dẫn tới secret chứ không phải
// secret — secretName/secretKeyRef của k8s, biến *_FILE/*_PATH (Docker
// secrets, file mount). file/path phải đứng sau dấu phân cách (_ . -) hoặc
// ranh giới camelCase (secretPath) — key chỉ tình cờ kết thúc bằng "file"
// như secretProfile không được tính. Đường dẫn chỉ được bỏ qua qua TÊN
// KEY, không qua hình dạng giá trị: password = "/Xk9..." vẫn phải bị báo.
var referenceKey = regexp.MustCompile(`^(?:(?i:.*secret(?:name|keyref))|.*(?:[_.-](?i:file|path)|[a-z0-9](?:File|Path)))$`)

// headerKey + headerNameValue: key *Header CHỈ là tham chiếu khi giá trị
// cũng có dạng tên header (apiKeyHeader = "X-Api-Key") — key kiểu
// authTokenHeader hay chứa luôn token thật, phải báo.
var (
	headerKey       = regexp.MustCompile(`(?i)header$`)
	headerNameValue = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:-[A-Za-z0-9]+)+$`)
)

// referenceValue: giá trị chỉ là tên env var (có "_", vd DB_PASSWORD — tên
// idiom Go const passwordEnv = "DB_PASSWORD"). Bắt buộc có "_" để secret
// thật toàn chữ hoa/số (HUNTER2024) vẫn bị báo.
var referenceValue = regexp.MustCompile(`^[A-Z][A-Z0-9]*_[A-Z0-9_]+$`)

func isReferenceAssignment(key, value string) bool {
	if headerKey.MatchString(key) {
		return headerNameValue.MatchString(value)
	}
	return referenceKey.MatchString(key) || referenceValue.MatchString(value)
}

// matches báo line có khớp p không, đã loại match tham chiếu (assignment).
func (p secretPattern) matches(line string) bool {
	if !p.assignment {
		return p.re.MatchString(line)
	}
	for _, m := range p.re.FindAllStringSubmatch(line, -1) {
		if !isReferenceAssignment(m[1], m[2]) {
			return true
		}
	}
	return false
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
					if p.matches(l.Content) {
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
