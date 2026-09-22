// Package githubapp xác thực với GitHub bằng GitHub App (JWT + installation
// access token) thay vì Personal Access Token tĩnh — xem issue #47.
package githubapp

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GenerateAppJWT ký 1 JSON Web Token ở CẤP APP (App-level JWT) theo đúng spec
// GitHub App: thuật toán RS256, claim "iss" là App ID.
//
// JWT này CHƯA dùng để gọi API trên 1 repo cụ thể được (vd post comment) —
// nó chỉ đủ quyền gọi endpoint "/app/..." để đổi lấy installation access
// token (bước tiếp theo, xem issue #47). Coi App-level JWT như "chứng minh
// mình là chính App này", còn installation token mới là "quyền hành động
// trên 1 installation/repo cụ thể".
//
// privateKeyPEM là nội dung file .pem GitHub cho tải về khi tạo App (định
// dạng PKCS#1 "RSA PRIVATE KEY" hoặc PKCS#8 "PRIVATE KEY" đều được, jwt-go
// tự nhận diện).
func GenerateAppJWT(appID string, privateKeyPEM []byte) (string, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("cannot parse private key: %w", err)
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		// iat lùi lại 60s để chừa sai lệch đồng hồ giữa máy mình và GitHub
		// (server GitHub thấy token "issued at" tương lai so với đồng hồ nó
		// sẽ từ chối) — đây là khuyến nghị chính thức của GitHub docs.
		IssuedAt: jwt.NewNumericDate(now.Add(-60 * time.Second)),
		// exp tối đa GitHub cho phép là 10 phút kể từ iat; đặt đúng 10 phút
		// kể từ "now" (chưa lùi) để có đủ thời gian dùng token trong lúc vẫn
		// nằm trong giới hạn.
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
		Issuer:    appID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		return "", fmt.Errorf("cannot sign jwt: %w", err)
	}

	return signed, nil
}
