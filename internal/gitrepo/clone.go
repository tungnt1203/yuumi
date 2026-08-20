package gitrepo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

func CloneRepo(repoFullName string, sha string) (dir string, cleanup func(), err error) {
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
