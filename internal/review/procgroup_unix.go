//go:build unix

package review

import (
	"os/exec"
	"syscall"
)

// killProcessGroupOnCancel cho cmd chạy trong process group riêng và khi
// context hết hạn thì kill cả group. exec.CommandContext mặc định chỉ kill
// tiến trình chính: go vet sinh thêm tiến trình compile/vet, nếu không kill
// cả group thì chúng thành mồ côi và tiếp tục ăn CPU/RAM của server sau
// timeout (issue #78).
func killProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
