package sandbox

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestLocal_CommandRunsInDirWithGivenEnv(t *testing.T) {
	dir := t.TempDir()
	env := []string{"FOO=bar"}
	cmd := Local(dir).Command(context.Background(), env, "echo", "hi")

	if cmd.Dir != dir {
		t.Errorf("Dir = %q, want %q", cmd.Dir, dir)
	}
	if !slices.Equal(cmd.Env, env) {
		t.Errorf("Env = %v, want %v", cmd.Env, env)
	}
	if !slices.Equal(cmd.Args, []string{"echo", "hi"}) {
		t.Errorf("Args = %v", cmd.Args)
	}
}

// containsSeq báo args có chứa đúng dãy liên tiếp seq không (vd flag + giá trị).
func containsSeq(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		if slices.Equal(args[i:i+len(seq)], seq) {
			return true
		}
	}
	return false
}

func TestRunArgs_LocksDownContainer(t *testing.T) {
	args := runArgs(DockerConfig{Image: "yuumi:test"}, "/work/clone-1", "yuumi-job-abc", 10001, 10001)

	for _, want := range [][]string{
		{"--user", "10001:10001"},
		{"--read-only"},
		{"--init"},
		{"--no-healthcheck"},
		{"--cap-drop", "ALL"},
		{"--security-opt", "no-new-privileges"},
		{"--memory", "2g"},
		{"--pids-limit", "512"},
		// Code PR mount chỉ đọc.
		{"--volume", "/work/clone-1:/work:ro"},
		{"--name", "yuumi-job-abc"},
		{"--entrypoint", "sleep", "yuumi:test"},
	} {
		if !containsSeq(args, want...) {
			t.Errorf("docker run args missing %v: %v", want, args)
		}
	}
	// Không env nào của server lúc tạo container, chỉ các giá trị cố định.
	for i, a := range args {
		if a == "--env" && !slices.Contains([]string{"HOME=/home/yuumi", "GOCACHE=/tmp/go-cache", "GOMODCACHE=/tmp/go-mod", "GOPATH=/tmp/go"}, args[i+1]) {
			t.Errorf("unexpected --env %q at docker run", args[i+1])
		}
	}
}

func TestDockerCommand_ForwardsOnlyAllowlistedEnvWithoutValuesInArgs(t *testing.T) {
	d := &docker{name: "yuumi-job-abc", dir: "/work/clone-1"}
	env := []string{
		"CLAUDE_CODE_OAUTH_TOKEN=secret-token",
		"GOTOOLCHAIN=local",
		"PATH=/server/path",
		"GITHUB_WEBHOOK_SECRET=server-secret",
		"SOME_NEW_SECRET=x",
	}
	cmd := d.Command(context.Background(), env, "claude", "-p", "hi")

	wantArgs := []string{"docker", "exec", "--workdir", "/work",
		"--env", "CLAUDE_CODE_OAUTH_TOKEN", "--env", "GOTOOLCHAIN",
		"yuumi-job-abc", "claude", "-p", "hi"}
	if !slices.Equal(cmd.Args, wantArgs) {
		t.Errorf("Args = %v\nwant %v", cmd.Args, wantArgs)
	}
	// Giá trị credential không được nằm trong args (lộ qua `ps`).
	if strings.Contains(strings.Join(cmd.Args, " "), "secret-token") {
		t.Errorf("credential value leaked into args: %v", cmd.Args)
	}
	// Giá trị đi qua env của chính lệnh docker, chỉ các biến trong allowlist.
	if !slices.Contains(cmd.Env, "CLAUDE_CODE_OAUTH_TOKEN=secret-token") {
		t.Errorf("Env missing forwarded credential: %v", cmd.Env)
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "GITHUB_WEBHOOK_SECRET=") || strings.HasPrefix(kv, "SOME_NEW_SECRET=") {
			t.Errorf("non-allowlisted var reached docker env: %q", kv)
		}
	}
}
