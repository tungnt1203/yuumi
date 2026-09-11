package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// sign tạo header "sha256=<hex>" giống cách GitHub ký request thật,
// dùng để build input hợp lệ cho các test case.
func sign(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	secret := "my-secret"
	payload := []byte(`{"action":"created"}`)

	tests := []struct {
		name    string
		secret  string
		payload []byte
		header  string
		want    bool
	}{
		{
			name:    "valid signature",
			secret:  secret,
			payload: payload,
			header:  sign(secret, payload),
			want:    true,
		},
		{
			name:    "wrong secret",
			secret:  "wrong-secret",
			payload: payload,
			header:  sign(secret, payload),
			want:    false,
		},
		{
			name:    "tampered payload",
			secret:  secret,
			payload: []byte(`{"action":"tampered"}`),
			header:  sign(secret, payload),
			want:    false,
		},
		{
			name:    "missing sha256 prefix",
			secret:  secret,
			payload: payload,
			header:  hex.EncodeToString([]byte("no-prefix")),
			want:    false,
		},
		{
			name:    "empty header",
			secret:  secret,
			payload: payload,
			header:  "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifySignature(tt.secret, tt.payload, tt.header)
			if got != tt.want {
				t.Errorf("VerifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}
