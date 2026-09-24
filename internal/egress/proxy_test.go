package egress

import (
	"bufio"
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
	p.dial = func(network, addr string) (net.Conn, error) {
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
	p.dial = func(network, addr string) (net.Conn, error) {
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
	p.dial = func(network, addr string) (net.Conn, error) { return nil, fmt.Errorf("no route") }
	srv := httptest.NewServer(p)
	defer srv.Close()

	conn, _, status := connect(t, srv.Listener.Addr().String(), "api.anthropic.com:443")
	conn.Close()
	if status != http.StatusBadGateway {
		t.Errorf("CONNECT status = %d, want 502", status)
	}
}
