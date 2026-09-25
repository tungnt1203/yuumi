package egress

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCredentialFromEnv(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}

	got, err := CredentialFromEnv(env(map[string]string{APIKeyEnv: "sk-real", OAuthTokenEnv: "oauth-real"}))
	if err != nil || got.Env != OAuthTokenEnv || got.Value != "oauth-real" {
		t.Errorf("both set: got %+v, %v; want OAuth token first (như Claude CLI)", got, err)
	}

	got, err = CredentialFromEnv(env(map[string]string{APIKeyEnv: "sk-real"}))
	if err != nil || got.Env != APIKeyEnv {
		t.Errorf("API key only: got %+v, %v", got, err)
	}

	if _, err := CredentialFromEnv(env(nil)); err == nil {
		t.Error("no credential: want error")
	}
}

// upstreamRequest là những gì "Anthropic" (server giả) nhận được.
type upstreamRequest struct {
	path, authorization, apiKey, body string
}

// withFakeAnthropic trỏ credential proxy sang server giả, trả channel nhận
// request mà server giả thấy.
func withFakeAnthropic(t *testing.T) <-chan upstreamRequest {
	t.Helper()
	got := make(chan upstreamRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- upstreamRequest{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"), string(body)}
		io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL)
	orig := anthropicAPI
	anthropicAPI = u
	t.Cleanup(func() { anthropicAPI = orig })
	return got
}

// Request từ sandbox mang credential giả (cả 2 loại header): upstream chỉ
// thấy credential thật, không thấy giá trị giả nào.
func TestCredentialProxy_ReplacesCredential(t *testing.T) {
	tests := []struct {
		name          string
		cred          Credential
		wantAuth      string
		wantAPIKeyHdr string
	}{
		{"oauth", Credential{Env: OAuthTokenEnv, Value: "oauth-real"}, "Bearer oauth-real", ""},
		{"api key", Credential{Env: APIKeyEnv, Value: "sk-real"}, "", "sk-real"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := withFakeAnthropic(t)
			proxy := NewCredentialProxy(tt.cred, nil)

			req := httptest.NewRequest(http.MethodPost, "http://yuumi-egress:3129/v1/messages", strings.NewReader(`{"model":"x"}`))
			req.Header.Set("Authorization", "Bearer yuumi-sandbox-placeholder")
			req.Header.Set("X-Api-Key", "yuumi-sandbox-placeholder")
			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			up := <-got
			if up.path != "/v1/messages" || up.body != `{"model":"x"}` {
				t.Errorf("upstream got path=%q body=%q", up.path, up.body)
			}
			if up.authorization != tt.wantAuth || up.apiKey != tt.wantAPIKeyHdr {
				t.Errorf("upstream auth=%q x-api-key=%q, want %q / %q", up.authorization, up.apiKey, tt.wantAuth, tt.wantAPIKeyHdr)
			}
			if strings.Contains(up.authorization+up.apiKey, "placeholder") {
				t.Error("placeholder credential reached upstream")
			}
		})
	}
}

// Chỉ /v1/ đã chuẩn hoá; path khác không được chuyển tiếp kèm credential thật.
func TestCredentialProxy_RejectsOtherPaths(t *testing.T) {
	got := withFakeAnthropic(t)
	proxy := NewCredentialProxy(Credential{Env: OAuthTokenEnv, Value: "oauth-real"}, nil)

	for _, p := range []string{"/", "/api/oauth/profile", "/v1/../api/oauth/profile", "/v1"} {
		req := httptest.NewRequest(http.MethodGet, "http://yuumi-egress:3129/", nil)
		req.URL.Path = p
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("path %q: status = %d, want 403", p, rec.Code)
		}
	}
	select {
	case up := <-got:
		t.Errorf("rejected path reached upstream: %+v", up)
	default:
	}
}
