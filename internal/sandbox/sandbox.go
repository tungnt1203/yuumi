// Package sandbox là nơi chạy lệnh trên code PR (gofmt/go vet, Claude CLI)
// của 1 job review. Code PR là input không tin cậy (issue #78): bản Docker
// cho mỗi job 1 container riêng, huỷ khi job xong; bản Local chạy thẳng
// trên máy server như trước (dev, test, hoặc khi chưa bật sandbox).
package sandbox

import (
	"context"
	"os/exec"
)

// Env chạy lệnh trên thư mục code PR của đúng 1 job. Gọi Close khi job xong.
type Env interface {
	// Dir là thư mục code PR trên máy server. Chỉ dùng để đọc file từ phía
	// server (vd .yuumi.yml) hoặc ghi log; lệnh trên code PR phải chạy qua
	// Command, không tự exec trong Dir.
	Dir() string

	// Command dựng lệnh name args chạy trong thư mục code PR. env là biến
	// môi trường của lệnh (caller đã lọc secret của server); bản Docker chỉ
	// chuyển vào container các biến trong forwardEnvKeys.
	Command(ctx context.Context, env []string, name string, args ...string) *exec.Cmd

	// Close giải phóng môi trường (bản Docker xoá container).
	Close() error
}

// Local chạy lệnh thẳng trên máy server, trong dir — hành vi trước khi có
// sandbox.
func Local(dir string) Env {
	return local{dir: dir}
}

type local struct {
	dir string
}

func (l local) Dir() string { return l.dir }

func (l local) Command(ctx context.Context, env []string, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = l.dir
	cmd.Env = env
	return cmd
}

func (l local) Close() error { return nil }
