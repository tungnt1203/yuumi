//go:build !unix

package review

import "os/exec"

// killProcessGroupOnCancel: ngoài Unix giữ hành vi mặc định của
// exec.CommandContext (chỉ kill tiến trình chính). Server chỉ deploy trên
// Linux/macOS; bản này để package vẫn build được trên Windows.
func killProcessGroupOnCancel(cmd *exec.Cmd) {}
