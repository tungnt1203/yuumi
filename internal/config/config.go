package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	WebhookSecret string
	GitHubToken   string
	AllowedUsers  []string

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

	token, ok := os.LookupEnv("GITHUB_TOKEN")
	if !ok {
		return Config{}, fmt.Errorf("GITHUB_TOKEN environment variable is required")
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
		GitHubToken:          token,
		AllowedUsers:         strings.Split(allowedUsersRaw, ","),
		MaxDiffBundleChars:   maxDiffBundleChars,
		MaxConcurrentReviews: maxConcurrentReviews,
		ReviewLogDir:         os.Getenv("REVIEW_LOG_DIR"),
		ReviewStateFile:      os.Getenv("REVIEW_STATE_FILE"),
	}, nil
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
