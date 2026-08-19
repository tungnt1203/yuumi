package config

import (
    "fmt"
    "os"
    "strings"
)

type Config struct {
    WebhookSecret string
    GitHubToken   string
    AllowedUsers  []string
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

    return Config{
        WebhookSecret: secret,
        GitHubToken:   token,
        AllowedUsers:  strings.Split(allowedUsersRaw, ","),
    }, nil
}
