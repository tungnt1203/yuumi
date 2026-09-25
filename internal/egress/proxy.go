// Package egress là HTTP proxy giới hạn đường ra Internet của container
// sandbox (issue #78, bước 2). Sandbox nằm trong Docker network --internal
// (không có route ra ngoài), chỉ đi được qua proxy này; proxy chỉ cho
// CONNECT tới cổng 443 của các host trong allowlist, mọi thứ khác bị chặn.
package egress

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultAllowedHosts là các host sandbox được CONNECT thẳng tới: chỉ Go
// module proxy cho go vet (xem review.goHardenedEnv: GOPROXY chỉ
// proxy.golang.org). Không cần sum.golang.org: GOFLAGS=-mod=readonly chỉ
// đối chiếu go.sum có sẵn (đã kiểm chứng bằng log proxy khi chạy go vet
// thật).
//
// api.anthropic.com KHÔNG nằm ở đây: Claude CLI gọi model qua credential
// proxy (NewCredentialProxy), sandbox không có đường thẳng tới Anthropic.
var DefaultAllowedHosts = []string{
	"proxy.golang.org",
}

// dialTimeout giới hạn thời gian kết nối tới host đích.
const dialTimeout = 10 * time.Second

// Proxy dùng chung cho mọi sandbox đang chạy, mà sandbox chạy code PR không
// tin cậy: 1 sandbox không được giữ tài nguyên proxy vô hạn.
const (
	// defaultIdleTimeout đóng tunnel khi CẢ 2 chiều không có dữ liệu trong
	// khoảng này. Tính chung 2 chiều: lúc chờ model trả lời, chiều gửi lên
	// im lặng lâu trong khi chiều nhận vẫn chạy.
	defaultIdleTimeout = 10 * time.Minute

	// defaultMaxTunnels giới hạn số tunnel mở cùng lúc; vượt thì từ chối
	// (503) thay vì mở thêm goroutine/fd.
	defaultMaxTunnels = 64
)

// Proxy là http.Handler xử lý CONNECT với allowlist host.
type Proxy struct {
	allowed map[string]bool

	// Logf ghi 1 dòng cho mỗi request (cho phép/chặn) — nil thì dùng
	// log.Printf. Test thay bằng hàm ghi lại.
	Logf func(format string, args ...any)

	// dial mở kết nối tới host đích; nil thì dùng net.Dialer thật. Nhận ctx
	// của request để huỷ dial khi sandbox ngắt kết nối giữa chừng.
	dial func(ctx context.Context, network, addr string) (net.Conn, error)

	// idleTimeout và tunnels (semaphore, sức chứa = số tunnel tối đa): New
	// đặt default, test thay giá trị nhỏ.
	idleTimeout time.Duration
	tunnels     chan struct{}
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
	return &Proxy{
		allowed:     allowed,
		idleTimeout: defaultIdleTimeout,
		tunnels:     make(chan struct{}, defaultMaxTunnels),
	}
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

	select {
	case p.tunnels <- struct{}{}:
		defer func() { <-p.tunnels }()
	default:
		p.logf("egress BUSY CONNECT %s (đã có %d tunnel)", r.Host, cap(p.tunnels))
		http.Error(w, "too many tunnels", http.StatusServiceUnavailable)
		return
	}

	dial := p.dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: dialTimeout}).DialContext
	}
	upstream, err := dial(r.Context(), "tcp", r.Host)
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
	// Conn sau Hijack có thể còn deadline của ReadHeaderTimeout: bỏ đi để
	// tunnel chỉ bị đóng theo idle timeout của pipe.
	client.SetDeadline(time.Time{})
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
	if pipe(client, upstream, p.idleTimeout) {
		p.logf("egress IDLE CONNECT %s: đóng sau %v không có dữ liệu", r.Host, p.idleTimeout)
	}
}

// pipe chuyển dữ liệu 2 chiều tới khi 1 bên đóng, hoặc cả 2 chiều không có
// dữ liệu trong idle (idled=true), rồi đóng cả 2.
func pipe(a, b net.Conn, idle time.Duration) (idled bool) {
	var lastActive atomic.Int64
	lastActive.Store(time.Now().UnixNano())
	done := make(chan struct{})
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			a.Close()
			b.Close()
			close(done)
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)
	copyDir := func(dst, src net.Conn) {
		defer wg.Done()
		// Đóng cả 2 khi 1 chiều kết thúc, để chiều còn lại (đang chờ đọc)
		// thoát ra.
		defer closeBoth()
		io.Copy(dst, activityReader{src, &lastActive})
	}
	go copyDir(a, b)
	go copyDir(b, a)

	// Watchdog: tunnel bị bỏ mặc (không ai gửi, không ai đóng) thì tự đóng.
	var timedOut atomic.Bool
	go func() {
		ticker := time.NewTicker(idle / 4)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if time.Since(time.Unix(0, lastActive.Load())) > idle {
					timedOut.Store(true)
					closeBoth()
					return
				}
			}
		}
	}()

	wg.Wait()
	return timedOut.Load()
}

// activityReader ghi lại thời điểm đọc được dữ liệu gần nhất (chung cho cả
// 2 chiều của tunnel).
type activityReader struct {
	net.Conn
	lastActive *atomic.Int64
}

func (r activityReader) Read(p []byte) (int, error) {
	n, err := r.Conn.Read(p)
	if n > 0 {
		r.lastActive.Store(time.Now().UnixNano())
	}
	return n, err
}
