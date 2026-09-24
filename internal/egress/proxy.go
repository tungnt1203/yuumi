// Package egress là HTTP proxy giới hạn đường ra Internet của container
// sandbox (issue #78, bước 2). Sandbox nằm trong Docker network --internal
// (không có route ra ngoài), chỉ đi được qua proxy này; proxy chỉ cho
// CONNECT tới cổng 443 của các host trong allowlist, mọi thứ khác bị chặn.
package egress

import (
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultAllowedHosts là các host review cần gọi ra ngoài: model của
// Claude CLI và Go module proxy cho go vet (xem review.goHardenedEnv:
// GOPROXY chỉ proxy.golang.org). Không cần sum.golang.org: GOFLAGS=
// -mod=readonly chỉ đối chiếu go.sum có sẵn (đã kiểm chứng bằng log proxy
// khi chạy go vet thật).
var DefaultAllowedHosts = []string{
	"api.anthropic.com",
	"proxy.golang.org",
}

// dialTimeout giới hạn thời gian kết nối tới host đích.
const dialTimeout = 10 * time.Second

// Proxy là http.Handler xử lý CONNECT với allowlist host.
type Proxy struct {
	allowed map[string]bool

	// Logf ghi 1 dòng cho mỗi request (cho phép/chặn) — nil thì dùng
	// log.Printf. Test thay bằng hàm ghi lại.
	Logf func(format string, args ...any)

	// dial mở kết nối tới host đích; nil thì dùng net.Dialer thật.
	dial func(network, addr string) (net.Conn, error)
}

// New tạo Proxy chỉ cho phép các host trong allowedHosts (so khớp chính
// xác, không phân biệt hoa thường; không có wildcard).
func New(allowedHosts []string) *Proxy {
	allowed := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			allowed[h] = true
		}
	}
	return &Proxy{allowed: allowed}
}

func (p *Proxy) logf(format string, args ...any) {
	if p.Logf != nil {
		p.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Allowed báo target (dạng host:port của CONNECT) có được phép không: chỉ
// cổng 443 và host nằm trong allowlist. Chỉ cho HTTPS nên proxy không phải
// đọc/sửa nội dung, cũng không mở được kết nối thô tới cổng tuỳ ý.
func (p *Proxy) Allowed(target string) bool {
	host, port, err := net.SplitHostPort(target)
	if err != nil || port != "443" {
		return false
	}
	return p.allowed[strings.ToLower(host)]
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		// HTTP thường (không mã hoá) không được hỗ trợ: mọi đích hợp lệ
		// đều là HTTPS.
		p.logf("egress DENY %s %s (chỉ hỗ trợ CONNECT)", r.Method, r.Host)
		http.Error(w, "only CONNECT is allowed", http.StatusMethodNotAllowed)
		return
	}
	if !p.Allowed(r.Host) {
		p.logf("egress DENY CONNECT %s", r.Host)
		http.Error(w, "destination not allowed", http.StatusForbidden)
		return
	}

	dial := p.dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: dialTimeout}).Dial
	}
	upstream, err := dial("tcp", r.Host)
	if err != nil {
		p.logf("egress FAIL CONNECT %s: %v", r.Host, err)
		http.Error(w, "cannot reach destination", http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	client, buf, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	p.logf("egress ALLOW CONNECT %s", r.Host)

	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		client.Close()
		upstream.Close()
		return
	}
	// buf có thể đã đọc trước vài byte của client (vd TLS ClientHello gửi
	// ngay sau CONNECT) — phải chuyển tiếp chúng trước.
	if n := buf.Reader.Buffered(); n > 0 {
		pending, _ := buf.Reader.Peek(n)
		if _, err := upstream.Write(pending); err != nil {
			client.Close()
			upstream.Close()
			return
		}
	}
	pipe(client, upstream)
}

// pipe chuyển dữ liệu 2 chiều tới khi 1 bên đóng, rồi đóng cả 2.
func pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	copyAndClose := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		// Đóng cả 2 để chiều còn lại (đang chờ đọc) thoát ra.
		dst.Close()
		src.Close()
	}
	go copyAndClose(a, b)
	go copyAndClose(b, a)
	wg.Wait()
}
