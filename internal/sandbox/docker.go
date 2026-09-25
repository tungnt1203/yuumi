package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tungnt1203/yuumi/internal/procenv"
)

// workDir là nơi thư mục code PR được mount (chỉ đọc) trong container.
const workDir = "/work"

// maxLifetime giới hạn thời gian sống của container: Close xoá container
// khi job xong, còn đây là lưới an toàn nếu server chết giữa chừng (container
// chạy `sleep`, hết giờ thì tự thoát và --rm xoá nó).
const maxLifetime = 2 * time.Hour

// startTimeout giới hạn `docker run` (tải image lần đầu có thể lâu).
const startTimeout = 2 * time.Minute

// forwardEnvKeys là các biến duy nhất được chuyển từ env của lệnh vào
// container. Allowlist thay vì blocklist: secret mới thêm vào server sau
// này không tự lọt vào sandbox. PATH/HOME của server cũng không được
// chuyển — container có PATH/HOME riêng của image.
var forwardEnvKeys = []string{
	// KHÔNG có credential Claude: sandbox chỉ có giá trị giả đặt lúc tạo
	// container (runArgs), credential thật nằm ở credential proxy.
	"DISABLE_AUTOUPDATER",
	// Env đã siết cho gofmt/go vet (xem review.goHardenedEnv).
	"GOTOOLCHAIN", "CGO_ENABLED", "GOPROXY", "GOFLAGS", "GOSUMDB",
	// Không chuyển HTTP(S)_PROXY/NO_PROXY của server: `docker exec -e` sẽ
	// ghi đè proxy egress đặt lúc tạo container (runArgs).
}

// dockerCLIEnvKeys là env mà chính lệnh `docker` trên máy server cần để nói
// chuyện với daemon.
var dockerCLIEnvKeys = []string{
	"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG",
	"DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY",
}

// DockerConfig cấu hình container sandbox.
type DockerConfig struct {
	// Image phải có sẵn các tool mà review chạy (go, git, claude) — dùng
	// chính image của server.
	Image string

	// CredentialEnv là loại credential Claude của server
	// (egress.OAuthTokenEnv hoặc egress.APIKeyEnv). Sandbox nhận biến này
	// với giá trị GIẢ cùng loại: Claude CLI chọn header xác thực theo loại
	// (Bearer hay x-api-key), credential proxy thay bằng giá trị thật.
	CredentialEnv string
}

// placeholderCredential là giá trị giả của credential trong sandbox. Chỉ để
// Claude CLI chịu khởi động và gửi đúng loại header; không dùng được với
// Anthropic API.
const placeholderCredential = "yuumi-sandbox-placeholder"

// StartDocker tạo 1 container sandbox cho thư mục code PR dir. dir phải là
// đường dẫn mà Docker daemon thấy được: server chạy trong container thì
// thư mục clone phải được mount từ host vào server ở CÙNG đường dẫn (xem
// README, mục sandbox).
func StartDocker(cfg DockerConfig, dir string) (Env, error) {
	name, err := containerName()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", runArgs(cfg, dir, name, os.Getuid(), os.Getgid())...)
	cmd.Env = procenv.Only(os.Environ(), dockerCLIEnvKeys...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker run sandbox: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return &docker{name: name, dir: dir}, nil
}

// runArgs dựng tham số `docker run` cho container sandbox:
//   - chạy bằng đúng uid/gid của server để đọc được thư mục clone (tạo bằng
//     os.MkdirTemp, quyền 0700);
//   - root filesystem và thư mục code chỉ đọc; chỉ /tmp và $HOME ghi được,
//     dạng tmpfs (cache Go, ~/.claude của CLI), mất khi container bị xoá;
//   - bỏ mọi capability, cấm leo quyền, giới hạn RAM/CPU/số tiến trình, để
//     1 PR ác ý không làm sập máy server;
//   - --init: PID 1 là tini thay vì `sleep`, để dọn tiến trình zombie mồ côi
//     của các lệnh exec (không thì chúng chiếm dần --pids-limit);
//   - --no-healthcheck: image dùng chung với server nên có HEALTHCHECK gọi
//     /health, trong sandbox không có server nên luôn báo unhealthy;
//   - không env nào của server: biến cần cho từng lệnh đi qua `docker exec`.
//   - mạng: chỉ network --internal NetworkName (không có route ra ngoài);
//     đường ra duy nhất là egress proxy qua HTTP(S)_PROXY, chỉ cho host
//     trong allowlist (xem SetupNetwork, package egress);
//   - CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: Claude CLI không thử gửi
//     telemetry (proxy cũng chặn, nhưng khỏi tốn kết nối bị từ chối);
//   - Claude CLI gọi model qua credential proxy (ANTHROPIC_BASE_URL, HTTP
//     thường trong network nội bộ, NO_PROXY để không đi vòng qua proxy
//     CONNECT) với credential giả; credential thật không vào sandbox.
func runArgs(cfg DockerConfig, dir, name string, uid, gid int) []string {
	return []string{
		"run", "--detach", "--rm",
		"--name", name,
		"--label", "yuumi.sandbox=1",
		"--user", fmt.Sprintf("%d:%d", uid, gid),
		"--read-only",
		"--tmpfs", "/tmp:size=1g",
		"--tmpfs", "/home/yuumi:size=256m",
		"--env", "HOME=/home/yuumi",
		"--env", "GOCACHE=/tmp/go-cache",
		"--env", "GOMODCACHE=/tmp/go-mod",
		"--env", "GOPATH=/tmp/go",
		"--network", NetworkName,
		"--env", "HTTPS_PROXY=" + egressProxyURL,
		"--env", "HTTP_PROXY=" + egressProxyURL,
		"--env", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"--env", "ANTHROPIC_BASE_URL=" + credentialProxyURL,
		"--env", "NO_PROXY=" + EgressContainerName,
		"--env", cfg.CredentialEnv + "=" + placeholderCredential,
		"--init",
		"--no-healthcheck",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--memory", "2g",
		"--cpus", "2",
		"--pids-limit", "512",
		"--volume", dir + ":" + workDir + ":ro",
		"--workdir", workDir,
		"--entrypoint", "sleep",
		cfg.Image,
		fmt.Sprintf("%d", int(maxLifetime.Seconds())),
	}
}

func containerName() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("cannot generate sandbox name: %w", err)
	}
	return "yuumi-job-" + hex.EncodeToString(b), nil
}

type docker struct {
	name string
	dir  string
}

func (d *docker) Dir() string { return d.dir }

// Command chạy lệnh bằng `docker exec`. Biến được chuyển dạng `-e KEY`
// (không kèm giá trị): docker lấy giá trị từ env của chính lệnh docker, nên
// credential không nằm trong args (ai trên máy server cũng thấy qua `ps`).
//
// Hết ctx thì chỉ lệnh `docker` trên máy server bị kill; tiến trình trong
// container có thể còn chạy tới khi Close xoá container — vẫn bị giới hạn
// bởi CPU/RAM/pids của container.
func (d *docker) Command(ctx context.Context, env []string, name string, args ...string) *exec.Cmd {
	forwarded := procenv.Only(env, forwardEnvKeys...)

	execArgs := []string{"exec", "--workdir", workDir}
	for _, kv := range forwarded {
		key, _, _ := strings.Cut(kv, "=")
		execArgs = append(execArgs, "--env", key)
	}
	execArgs = append(execArgs, d.name, name)
	execArgs = append(execArgs, args...)

	cmd := exec.CommandContext(ctx, "docker", execArgs...)
	cmd.Env = append(procenv.Only(os.Environ(), dockerCLIEnvKeys...), forwarded...)
	return cmd
}

// Close xoá container (kill mọi tiến trình còn chạy trong đó).
func (d *docker) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "rm", "--force", d.name)
	cmd.Env = procenv.Only(os.Environ(), dockerCLIEnvKeys...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker rm sandbox %s: %w: %s", d.name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
