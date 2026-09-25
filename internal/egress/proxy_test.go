package egress

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAllowed(t *testing.T) {
	p := New([]string{"api.anthropic.com", " Proxy.Golang.org "})
	tests := []struct {
		target string
		want   bool
	}{
		{"api.anthropic.com:443", true},
		{"API.Anthropic.com:443", true},
		{"proxy.golang.org:443", true},
		// Chỉ cổng 443.
		{"api.anthropic.com:80", false},
		{"api.anthropic.com:22", false},
		// Không có wildcard: subdomain hay domain chứa tên hợp lệ đều bị chặn.
		{"evil.api.anthropic.com:443", false},
		{"api.anthropic.com.evil.com:443", false},
		{"example.com:443", false},
		{"api.anthropic.com", false},
	}
	for _, tt := range tests {
		if got := p.Allowed(tt.target); got != tt.want {
			t.Errorf("Allowed(%q) = %v, want %v", tt.target, got, tt.want)
		}
	}
}

// connect gửi CONNECT target qua proxy tại proxyAddr, trả conn (để dùng
// tunnel tiếp) và status code proxy trả về.
func connect(t *testing.T, proxyAddr, target string) (net.Conn, *bufio.Reader, int) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", proxyAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	return conn, br, resp.StatusCode
}

func TestServeHTTP_TunnelsAllowedHost(t *testing.T) {
	// Upstream giả: echo lại 1 dòng, thay cho api.anthropic.com:443.
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		c, err := upstream.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadString('\n')
		io.WriteString(c, "echo: "+line)
	}()

	p := New([]string{"api.anthropic.com"})
	p.Logf = func(string, ...any) {}
	var dialed string
	p.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = addr
		return net.Dial("tcp", upstream.Addr().String())
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	conn, br, status := connect(t, srv.Listener.Addr().String(), "api.anthropic.com:443")
	defer conn.Close()
	if status != http.StatusOK {
		t.Fatalf("CONNECT status = %d, want 200", status)
	}
	if dialed != "api.anthropic.com:443" {
		t.Errorf("dialed %q, want api.anthropic.com:443", dialed)
	}

	io.WriteString(conn, "hello\n")
	got, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read through tunnel: %v", err)
	}
	if got != "echo: hello\n" {
		t.Errorf("tunnel returned %q, want %q", got, "echo: hello\n")
	}
}

func TestServeHTTP_DeniesHostNotInAllowlist(t *testing.T) {
	p := New([]string{"api.anthropic.com"})
	var logs []string
	p.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	p.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		t.Errorf("must not dial denied host, dialed %q", addr)
		return nil, fmt.Errorf("unexpected dial")
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	conn, _, status := connect(t, srv.Listener.Addr().String(), "example.com:443")
	conn.Close()
	if status != http.StatusForbidden {
		t.Errorf("CONNECT status = %d, want 403", status)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "DENY CONNECT example.com:443") {
		t.Errorf("logs = %v, want a DENY line", logs)
	}
}

// HTTP thường (không CONNECT) bị từ chối, kể cả tới host hợp lệ: proxy không
// làm forward proxy cho request không mã hoá.
func TestServeHTTP_RejectsPlainHTTP(t *testing.T) {
	p := New([]string{"api.anthropic.com"})
	p.Logf = func(string, ...any) {}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.anthropic.com/v1/messages", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestServeHTTP_UpstreamDialErrorIsBadGateway(t *testing.T) {
	p := New([]string{"api.anthropic.com"})
	p.Logf = func(string, ...any) {}
	p.dial = func(ctx context.Context, network, addr string) (net.Conn, error) { return nil, fmt.Errorf("no route") }
	srv := httptest.NewServer(p)
	defer srv.Close()

	conn, _, status := connect(t, srv.Listener.Addr().String(), "api.anthropic.com:443")
	conn.Close()
	if status != http.StatusBadGateway {
		t.Errorf("CONNECT status = %d, want 502", status)
	}
}

// Tunnel mà cả 2 chiều im lặng quá idleTimeout thì tự đóng: 1 sandbox không
// giữ được kết nối trên proxy dùng chung mãi mãi.
func TestPipe_ClosesIdleTunnel(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()
	defer a2.Close()
	defer b2.Close()

	start := time.Now()
	idled := pipe(a1, b1, 80*time.Millisecond)
	if !idled {
		t.Error("pipe() idled = false, want true")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("idle tunnel closed after %v, want ~80ms", elapsed)
	}
}

// Chỉ 1 chiều có dữ liệu (vd đang nhận response dài, chiều gửi im lặng)
// thì tunnel KHÔNG bị coi là idle.
func TestPipe_OneWayTrafficKeepsTunnelOpen(t *testing.T) {
	client, clientPeer := net.Pipe()
	upstream, upstreamPeer := net.Pipe()

	result := make(chan bool, 1)
	go func() { result <- pipe(clientPeer, upstreamPeer, 100*time.Millisecond) }()

	// Upstream gửi đều đặn trong ~400ms (gấp 4 lần idle), client chỉ đọc.
	go io.Copy(io.Discard, client)
	for i := 0; i < 8; i++ {
		if _, err := upstream.Write([]byte("x")); err != nil {
			t.Fatalf("tunnel closed while upstream still sending (write %d): %v", i, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	upstream.Close()

	select {
	case idled := <-result:
		if idled {
			t.Error("pipe() idled = true, want false (tunnel had traffic)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pipe() did not return after upstream closed")
	}
}

func TestServeHTTP_RejectsWhenTooManyTunnels(t *testing.T) {
	p := New([]string{"api.anthropic.com"})
	p.Logf = func(string, ...any) {}
	p.tunnels = make(chan struct{}, 1)
	p.tunnels <- struct{}{} // 1 tunnel đang mở, hết chỗ
	p.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		t.Error("must not dial when tunnel limit is reached")
		return nil, fmt.Errorf("unexpected dial")
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	conn, _, status := connect(t, srv.Listener.Addr().String(), "api.anthropic.com:443")
	conn.Close()
	if status != http.StatusServiceUnavailable {
		t.Errorf("CONNECT status = %d, want 503", status)
	}
}

// Dial nhận ctx của request: sandbox ngắt kết nối thì dial bị huỷ.
func TestServeHTTP_DialUsesRequestContext(t *testing.T) {
	p := New([]string{"api.anthropic.com"})
	p.Logf = func(string, ...any) {}
	cancelled := make(chan struct{})
	p.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}
	srv := httptest.NewServer(p)
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(conn, "CONNECT api.anthropic.com:443 HTTP/1.1\r\nHost: api.anthropic.com:443\r\n\r\n")
	time.Sleep(50 * time.Millisecond)
	conn.Close()

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("dial was not cancelled after client disconnected")
	}
}
