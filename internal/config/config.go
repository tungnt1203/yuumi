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

	maxDiffBundleChars := 0
	if raw, ok := os.LookupEnv("MAX_DIFF_BUNDLE_CHARS"); ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("MAX_DIFF_BUNDLE_CHARS must be a positive integer, got %q", raw)
		}
		maxDiffBundleChars = n
	}

	return Config{
		WebhookSecret:      secret,
		GitHubToken:        token,
		AllowedUsers:       strings.Split(allowedUsersRaw, ","),
		MaxDiffBundleChars: maxDiffBundleChars,
	}, nil
}
