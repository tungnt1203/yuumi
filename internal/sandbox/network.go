package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/tungnt1203/yuumi/internal/egress"
	"github.com/tungnt1203/yuumi/internal/procenv"
)

const (
	// NetworkName là Docker network --internal (không có route ra Internet)
	// mà mọi container sandbox nằm trong (issue #78, bước 2).
	NetworkName = "yuumi-sandbox"

	// EgressContainerName là container chạy egress proxy (cmd/egressproxy):
	// nối vào cả NetworkName lẫn bridge mặc định, là đường ra duy nhất của
	// sandbox, chỉ cho các host trong allowlist.
	EgressContainerName = "yuumi-egress"

	// egressProxyURL là địa chỉ proxy nhìn từ trong sandbox (Docker DNS
	// phân giải tên container trong cùng network).
	egressProxyURL = "http://" + EgressContainerName + ":3128"

	// credentialProxyURL là credential proxy tới Anthropic API, cùng
	// container egress (issue #78, bước 3).
	credentialProxyURL = "http://" + EgressContainerName + ":3129"
)

// credentialEnvKeys là credential Claude mà container egress nhận từ env
// của server (dạng `--env KEY`, giá trị không nằm trong args).
var credentialEnvKeys = []string{egress.OAuthTokenEnv, egress.APIKeyEnv}

// dockerRunner chạy 1 lệnh docker, trả stdout. Tách ra để test logic của
// SetupNetwork mà không cần Docker thật.
type dockerRunner func(args ...string) (string, error)

func runDocker(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	// Kèm credential Claude: `docker run --env KEY` của container egress lấy
	// giá trị từ đây. Các lệnh docker khác không chuyển gì vào container.
	cmd.Env = procenv.Only(os.Environ(), slices.Concat(dockerCLIEnvKeys, credentialEnvKeys)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// SetupNetwork chuẩn bị mạng cho sandbox, gọi 1 lần lúc server khởi động:
//  1. network NetworkName, tạo mới với --internal nếu chưa có;
//  2. container egress proxy, luôn tạo lại từ image hiện tại (để proxy cùng
//     phiên bản với server), rồi nối vào NetworkName.
//
// Network cùng tên đã có sẵn mà KHÔNG phải --internal thì trả lỗi thay vì
// dùng tiếp: sandbox trong network đó ra Internet tự do mà không ai biết.
func SetupNetwork(image string) error {
	return setupNetwork(runDocker, image)
}

func setupNetwork(run dockerRunner, image string) error {
	internal, err := run("network", "inspect", "--format", "{{.Internal}}", NetworkName)
	switch {
	case err != nil:
		// Chưa có network (hoặc inspect lỗi): tạo mới. Lỗi thật (daemon
		// không chạy...) sẽ lộ ra ở lệnh create.
		if _, err := run("network", "create", "--internal", NetworkName); err != nil {
			return fmt.Errorf("tạo network sandbox: %w", err)
		}
	case internal != "true":
		return fmt.Errorf("network %q đã có nhưng không phải --internal (Internal=%s): sandbox sẽ ra Internet tự do. Xoá nó (docker network rm %s) để server tạo lại", NetworkName, internal, NetworkName)
	}

	// Xoá container cũ (nếu có) để chạy đúng image hiện tại. Kiểm tra tồn
	// tại trước bằng `ps --filter` (trả ID hoặc rỗng) thay vì đoán qua
	// message lỗi của `rm`: exit code của `rm --force` khi container chưa có
	// khác nhau giữa các bản docker CLI, còn câu chữ lỗi không ổn định.
	existing, err := run("ps", "--all", "--quiet", "--filter", "name=^"+EgressContainerName+"$")
	if err != nil {
		return fmt.Errorf("kiểm tra egress proxy cũ: %w", err)
	}
	if existing != "" {
		if _, err := run("rm", "--force", EgressContainerName); err != nil {
			return fmt.Errorf("xoá egress proxy cũ: %w", err)
		}
	}
	if _, err := run(egressRunArgs(image)...); err != nil {
		return fmt.Errorf("chạy egress proxy: %w", err)
	}
	if _, err := run("network", "connect", NetworkName, EgressContainerName); err != nil {
		return fmt.Errorf("nối egress proxy vào network sandbox: %w", err)
	}
	return nil
}

// egressRunArgs dựng `docker run` cho container egress proxy: nằm ở bridge
// mặc định (có Internet), khoá quyền như sandbox. Proxy không đụng code PR
// nhưng nhận kết nối từ sandbox, nên cũng không cần quyền gì. Đây là nơi
// duy nhất giữ credential Claude thật ngoài server (credential proxy).
func egressRunArgs(image string) []string {
	args := []string{
		"run", "--detach",
		"--name", EgressContainerName,
		"--restart", "unless-stopped",
		"--label", "yuumi.egress=1",
		"--user", "10001:10001",
		"--read-only",
		"--init",
		"--no-healthcheck",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--memory", "256m",
		"--pids-limit", "256",
	}
	for _, key := range credentialEnvKeys {
		args = append(args, "--env", key)
	}
	return append(args, "--entrypoint", "yuumi-egressproxy", image)
}
