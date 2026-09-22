package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	WebhookSecret string
	AllowedUsers  []string

	// GitHubAppID + GitHubAppPrivateKey xác thực bot với GitHub qua GitHub
	// App (JWT + installation access token, xem package githubapp) thay vì
	// 1 Personal Access Token tĩnh dùng chung cho mọi repo (issue #47).
	GitHubAppID         string
	GitHubAppPrivateKey []byte

	// MaxDiffBundleChars override ngưỡng chia bundle của review.Job (xem
	// review.defaultBundleBudgetChars) — 0 nghĩa là "không set", để review
	// package tự dùng default của nó. Không bắt buộc: hầu hết môi trường
	// không cần set biến này.
	MaxDiffBundleChars int

	// MaxConcurrentReviews override số job review chạy đồng thời tối đa
	// (xem review.NewDispatcher/defaultMaxConcurrentJobs) — 0 nghĩa là
	// "không set", để review package tự dùng default của nó.
	MaxConcurrentReviews int

	// ReviewLogDir override thư mục ghi log request/response mỗi lần review
	// (xem reviewlog.FileLogger, issue #9) — rỗng nghĩa là "không set", để
	// reviewlog tự dùng default của nó.
	ReviewLogDir string

	// ReviewStateFile override đường dẫn file lưu SHA đã review lần gần
	// nhất cho mỗi PR (xem reviewstate.FileStore, issue #21) — rỗng nghĩa
	// là "không set", để reviewstate tự dùng default của nó.
	ReviewStateFile string
}

func Load() (Config, error) {
	secret, ok := os.LookupEnv("GITHUB_WEBHOOK_SECRET")
	if !ok {
		return Config{}, fmt.Errorf("GITHUB_WEBHOOK_SECRET environment variable is required")
	}

	appID, ok := os.LookupEnv("GITHUB_APP_ID")
	if !ok {
		return Config{}, fmt.Errorf("GITHUB_APP_ID environment variable is required")
	}

	privateKey, err := loadGitHubAppPrivateKey()
	if err != nil {
		return Config{}, err
	}

	allowedUsersRaw, ok := os.LookupEnv("ALLOWED_USERS")
	if !ok {
		return Config{}, fmt.Errorf("ALLOWED_USERS environment variable is required")
	}

	maxDiffBundleChars, err := parseOptionalPositiveIntEnv("MAX_DIFF_BUNDLE_CHARS")
	if err != nil {
		return Config{}, err
	}

	maxConcurrentReviews, err := parseOptionalPositiveIntEnv("MAX_CONCURRENT_REVIEWS")
	if err != nil {
		return Config{}, err
	}

	return Config{
		WebhookSecret:        secret,
		GitHubAppID:          appID,
		GitHubAppPrivateKey:  privateKey,
		AllowedUsers:         strings.Split(allowedUsersRaw, ","),
		MaxDiffBundleChars:   maxDiffBundleChars,
		MaxConcurrentReviews: maxConcurrentReviews,
		ReviewLogDir:         os.Getenv("REVIEW_LOG_DIR"),
		ReviewStateFile:      os.Getenv("REVIEW_STATE_FILE"),
	}, nil
}

// loadGitHubAppPrivateKey đọc private key (PEM) của GitHub App từ 1 trong 2
// nguồn, ưu tiên file trước:
//   - GITHUB_APP_PRIVATE_KEY_PATH: đường dẫn tới file .pem thô, đúng như
//     GitHub cho tải về lúc tạo App — tiện cho local dev, không cần encode
//     gì thêm.
//   - GITHUB_APP_PRIVATE_KEY: nội dung PEM encode base64 thành 1 dòng —
//     tiện set qua biến môi trường trên các nền tảng deploy (Docker, AWS...)
//     vì PEM gốc có xuống dòng, dễ bị escape sai nếu nhét thẳng vào env.
//
// Thiếu cả 2 hoặc GITHUB_APP_PRIVATE_KEY không phải base64 hợp lệ đều là lỗi
// cấu hình rõ ràng, fail ngay lúc khởi động thay vì để lỗi lộ ra khi có
// webhook đầu tiên.
func loadGitHubAppPrivateKey() ([]byte, error) {
	if path, ok := os.LookupEnv("GITHUB_APP_PRIVATE_KEY_PATH"); ok {
		key, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("cannot read GITHUB_APP_PRIVATE_KEY_PATH: %w", err)
		}
		return key, nil
	}

	if encoded, ok := os.LookupEnv("GITHUB_APP_PRIVATE_KEY"); ok {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY must be base64-encoded PEM: %w", err)
		}
		return key, nil
	}

	return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY_PATH or GITHUB_APP_PRIVATE_KEY environment variable is required")
}

// parseOptionalPositiveIntEnv đọc 1 biến môi trường optional dạng số nguyên
// dương. Trả về 0 (không lỗi) nếu biến chưa set — 0 được các package dùng
// Config coi là "chưa cấu hình, tự dùng default riêng của nó". Nếu biến CÓ
// set nhưng không phải số nguyên dương thì coi là lỗi cấu hình rõ ràng
// (thà fail sớm lúc khởi động còn hơn âm thầm dùng default sai ý người set).
func parseOptionalPositiveIntEnv(name string) (int, error) {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, raw)
	}
	return n, nil
}
