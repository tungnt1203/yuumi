package githubapp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// generateTestPrivateKeyPEM sinh 1 RSA key tạm trong bộ nhớ (không phải key
// thật của App nào) để test ký/verify JWT mà không cần file .pem thật.
func generateTestPrivateKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("cannot generate rsa key: %v", err)
	}

	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return pem.EncodeToMemory(block)
}

func TestGenerateAppJWT_ClaimsCorrect(t *testing.T) {
	privateKeyPEM := generateTestPrivateKeyPEM(t)

	signed, err := GenerateAppJWT("12345", privateKeyPEM)
	if err != nil {
		t.Fatalf("GenerateAppJWT() error = %v", err)
	}

	// Parse lại bằng public key rút ra từ chính private key test ở trên, để
	// xác nhận: (1) ký đúng RS256, verify được; (2) claim "iss" đúng App ID;
	// (3) exp - iat đúng ~10 phút như GitHub yêu cầu.
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		t.Fatalf("cannot parse test private key: %v", err)
	}

	claims := jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(signed, &claims, func(token *jwt.Token) (any, error) {
		return &key.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("cannot parse signed jwt: %v", err)
	}
	if !parsed.Valid {
		t.Fatalf("parsed jwt is not valid")
	}

	if claims.Issuer != "12345" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "12345")
	}

	gotDuration := claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time)
	wantDuration := 11 * time.Minute // 10 phút exp + 1 phút lùi iat
	if gotDuration < wantDuration-time.Second || gotDuration > wantDuration+time.Second {
		t.Errorf("exp - iat = %v, want ~%v", gotDuration, wantDuration)
	}
}

func TestGenerateAppJWT_InvalidPrivateKey(t *testing.T) {
	_, err := GenerateAppJWT("12345", []byte("not a valid pem"))
	if err == nil {
		t.Fatal("GenerateAppJWT() with invalid key: expected error, got nil")
	}
}
