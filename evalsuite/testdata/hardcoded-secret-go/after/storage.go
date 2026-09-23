package storage

import (
	"fmt"
	"os"
)

// Config là cấu hình kết nối object storage, đọc từ biến môi trường.
type Config struct {
	Bucket          string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
}

// LoadConfig đọc cấu hình từ biến môi trường, lỗi nếu thiếu bucket.
func LoadConfig() (Config, error) {
	cfg := Config{
		Bucket:          os.Getenv("STORAGE_BUCKET"),
		Region:          os.Getenv("STORAGE_REGION"),
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	if cfg.Bucket == "" {
		return Config{}, fmt.Errorf("STORAGE_BUCKET is required")
	}
	if cfg.Region == "" {
		cfg.Region = "ap-southeast-1"
	}
	return cfg, nil
}
