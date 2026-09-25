package sandbox

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
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
	args := runArgs(DockerConfig{Image: "yuumi:test", CredentialEnv: "CLAUDE_CODE_OAUTH_TOKEN"}, "/work/clone-1", "yuumi-job-abc", 10001, 10001)

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
		// Chỉ network internal, đường ra duy nhất là egress proxy.
		{"--network", "yuumi-sandbox"},
		{"--env", "HTTPS_PROXY=http://yuumi-egress:3128"},
		// Claude gọi model qua credential proxy, với credential GIẢ cùng loại.
		{"--env", "ANTHROPIC_BASE_URL=http://yuumi-egress:3129"},
		{"--env", "NO_PROXY=yuumi-egress"},
		{"--env", "CLAUDE_CODE_OAUTH_TOKEN=yuumi-sandbox-placeholder"},
	} {
		if !containsSeq(args, want...) {
			t.Errorf("docker run args missing %v: %v", want, args)
		}
	}
	// Không env nào của server lúc tạo container, chỉ các giá trị cố định.
	for i, a := range args {
		if a == "--env" && !slices.Contains([]string{
			"HOME=/home/yuumi", "GOCACHE=/tmp/go-cache", "GOMODCACHE=/tmp/go-mod", "GOPATH=/tmp/go",
			"HTTPS_PROXY=http://yuumi-egress:3128", "HTTP_PROXY=http://yuumi-egress:3128",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
			"ANTHROPIC_BASE_URL=http://yuumi-egress:3129", "NO_PROXY=yuumi-egress",
			"CLAUDE_CODE_OAUTH_TOKEN=yuumi-sandbox-placeholder",
		}, args[i+1]) {
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
		// Proxy của server không được ghi đè egress proxy của sandbox.
		"HTTPS_PROXY=http://corp-proxy:8080",
	}
	cmd := d.Command(context.Background(), env, "claude", "-p", "hi")

	// Credential thật KHÔNG được chuyển vào sandbox (issue #78 bước 3):
	// `docker exec -e` sẽ ghi đè credential giả đặt lúc tạo container.
	wantArgs := []string{"docker", "exec", "--workdir", "/work",
		"--env", "GOTOOLCHAIN",
		"yuumi-job-abc", "claude", "-p", "hi"}
	if !slices.Equal(cmd.Args, wantArgs) {
		t.Errorf("Args = %v\nwant %v", cmd.Args, wantArgs)
	}
	if strings.Contains(strings.Join(cmd.Args, " "), "secret-token") {
		t.Errorf("credential value leaked into args: %v", cmd.Args)
	}
	// Giá trị đi qua env của chính lệnh docker, chỉ các biến trong allowlist.
	if !slices.Contains(cmd.Env, "GOTOOLCHAIN=local") {
		t.Errorf("Env missing forwarded GOTOOLCHAIN: %v", cmd.Env)
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "CLAUDE_CODE_OAUTH_TOKEN=") || strings.HasPrefix(kv, "GITHUB_WEBHOOK_SECRET=") || strings.HasPrefix(kv, "SOME_NEW_SECRET=") || kv == "HTTPS_PROXY=http://corp-proxy:8080" {
			t.Errorf("non-allowlisted var reached docker env: %q", kv)
		}
	}
}

// ctx có deadline thì lệnh được bọc trong `timeout` để tiến trình trong
// container tự dừng khi hết giờ, không chỉ lệnh docker exec phía server
// (issue #98).
func TestDockerCommand_DeadlineWrapsWithTimeout(t *testing.T) {
	d := &docker{name: "yuumi-job-abc", dir: "/work/clone-1"}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := d.Command(ctx, nil, "claude", "-p", "hi")

	wantArgs := []string{"docker", "exec", "--workdir", "/work",
		"yuumi-job-abc", "timeout", "--kill-after=10s", "90s", "claude", "-p", "hi"}
	if !slices.Equal(cmd.Args, wantArgs) {
		t.Errorf("Args = %v\nwant %v", cmd.Args, wantArgs)
	}
}

// Deadline đã qua vẫn cho `timeout` tối thiểu 1s, không phải 0s (GNU
// timeout coi 0 là "không giới hạn").
func TestTimeoutPrefix_ExpiredDeadlineUsesOneSecond(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	got := timeoutPrefix(ctx)
	if len(got) != 3 || got[2] != "1s" {
		t.Errorf("timeoutPrefix() = %v, want duration 1s", got)
	}
}
