package gitrepo

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// CloneRepo fetch đúng commit sha của repoFullName vào 1 thư mục tạm.
//
// token (installation token của GitHub App) dùng để đọc repo private (issue
// #48); rỗng thì fetch không xác thực, chỉ được repo public. Token chỉ đi
// qua env của riêng lệnh `git fetch` (xem authEnv), KHÔNG nhúng vào URL
// remote: URL remote nằm trong .git/config của thư mục clone, mà thư mục đó
// được giao cho Claude CLI đọc — code PR không tin cậy có thể dụ Claude đọc
// ra token.
func CloneRepo(repoFullName string, sha string, token string) (dir string, cleanup func(), err error) {
	repoURL := fmt.Sprintf("https://github.com/%s.git", repoFullName)
	dir, err = os.MkdirTemp("", "yuumi-review-*")
	if err != nil {
		return "", nil, err
	}

	cleanup = func() { os.RemoveAll(dir) }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "init", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, err
	}

	cmd = exec.CommandContext(ctx, "git", "-C", dir, "remote", "add", "origin", repoURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, err
	}

	cmd = exec.CommandContext(ctx, "git", "-C", dir, "fetch", "--depth", "1", "origin", sha)
	cmd.Env = append(os.Environ(), authEnv(token)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, err
	}

	cmd = exec.CommandContext(ctx, "git", "-C", dir, "checkout", sha)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, err
	}

	return dir, cleanup, nil
}

// authEnv trả env để git gửi token dưới dạng header Authorization cho
// github.com. Dùng GIT_CONFIG_COUNT/KEY/VALUE (git >= 2.31) thay vì
// `-c http.extraheader=...` để token không nằm trong args của tiến trình
// (ai trên máy cũng thấy qua `ps`) và không bị ghi xuống .git/config.
//
// GIT_TERMINAL_PROMPT=0 luôn được set: thiếu/sai token với repo private thì
// git fail ngay, không treo chờ nhập username/password tới hết timeout.
func authEnv(token string) []string {
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	if token == "" {
		return env
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return append(env,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+basic,
	)
}
