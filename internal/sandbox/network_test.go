package sandbox

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// fakeDocker ghi lại các lệnh docker và trả kết quả theo lệnh con.
type fakeDocker struct {
	calls   [][]string
	inspect string // stdout của `network inspect`; "" = chưa có network
	egress  string // stdout của `ps --filter` tìm egress; "" = chưa có container
	errs    map[string]error
}

func (f *fakeDocker) run(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	key := strings.Join(args[:min(2, len(args))], " ")
	if err := f.errs[key]; err != nil {
		return "", err
	}
	if key == "ps --all" {
		return f.egress, nil
	}
	if key == "network inspect" {
		if f.inspect == "" {
			return "", errors.New("No such network")
		}
		return f.inspect, nil
	}
	return "", nil
}

func (f *fakeDocker) called(prefix ...string) bool {
	for _, c := range f.calls {
		if len(c) >= len(prefix) && slices.Equal(c[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

// assertEgressRecreated kiểm tra egress proxy được chạy từ image hiện tại
// và nối vào network sandbox.
func assertEgressRecreated(t *testing.T, f *fakeDocker) {
	t.Helper()
	for _, want := range [][]string{
		{"run", "--detach", "--name", "yuumi-egress"},
		{"network", "connect", "yuumi-sandbox", "yuumi-egress"},
	} {
		if !f.called(want...) {
			t.Errorf("missing docker %v: %v", want, f.calls)
		}
	}
}

func TestSetupNetwork_CreatesInternalNetworkAndEgress(t *testing.T) {
	f := &fakeDocker{}
	if err := setupNetwork(f.run, "yuumi:dev"); err != nil {
		t.Fatalf("setupNetwork() error = %v", err)
	}
	if !f.called("network", "create", "--internal", "yuumi-sandbox") {
		t.Errorf("network not created with --internal: %v", f.calls)
	}
	assertEgressRecreated(t, f)
}

// Lần chạy đầu trên máy mới: chưa có egress container thì không gọi `rm`
// (exit code của `rm --force` khi container chưa có khác nhau giữa các bản
// docker CLI).
func TestSetupNetwork_MissingEgressContainerSkipsRemove(t *testing.T) {
	f := &fakeDocker{}
	if err := setupNetwork(f.run, "yuumi:dev"); err != nil {
		t.Fatalf("setupNetwork() error = %v, want nil", err)
	}
	if f.called("rm") {
		t.Errorf("must not rm a container that does not exist: %v", f.calls)
	}
	assertEgressRecreated(t, f)
}

// Egress cũ còn (server khởi động lại): xoá rồi chạy lại từ image hiện tại.
func TestSetupNetwork_ExistingEgressIsReplaced(t *testing.T) {
	f := &fakeDocker{egress: "3f2a9c1b7d4e"}
	if err := setupNetwork(f.run, "yuumi:dev"); err != nil {
		t.Fatalf("setupNetwork() error = %v", err)
	}
	if !f.called("rm", "--force", "yuumi-egress") {
		t.Errorf("old egress container not removed: %v", f.calls)
	}
	assertEgressRecreated(t, f)
}

func TestSetupNetwork_ReusesExistingInternalNetwork(t *testing.T) {
	f := &fakeDocker{inspect: "true"}
	if err := setupNetwork(f.run, "yuumi:dev"); err != nil {
		t.Fatalf("setupNetwork() error = %v", err)
	}
	if f.called("network", "create") {
		t.Errorf("must not recreate an existing internal network: %v", f.calls)
	}
	// Egress proxy vẫn luôn được tạo lại từ image hiện tại.
	assertEgressRecreated(t, f)
}

// Network trùng tên nhưng không internal: sandbox trong đó sẽ ra Internet
// tự do, phải báo lỗi chứ không dùng tiếp.
func TestSetupNetwork_RejectsNonInternalNetwork(t *testing.T) {
	f := &fakeDocker{inspect: "false"}
	err := setupNetwork(f.run, "yuumi:dev")
	if err == nil || !strings.Contains(err.Error(), "không phải --internal") {
		t.Fatalf("setupNetwork() error = %v, want non-internal error", err)
	}
	if f.called("run") {
		t.Errorf("must not start egress on a non-internal network: %v", f.calls)
	}
}

func TestSetupNetwork_PropagatesErrors(t *testing.T) {
	for _, step := range []string{"network create", "ps --all", "rm --force", "run --detach", "network connect"} {
		t.Run(step, func(t *testing.T) {
			// egress có sẵn để bước rm thực sự được gọi.
			f := &fakeDocker{egress: "3f2a9c1b7d4e", errs: map[string]error{step: errors.New("daemon down")}}
			if err := setupNetwork(f.run, "yuumi:dev"); err == nil {
				t.Errorf("setupNetwork() = nil, want error when %q fails", step)
			}
		})
	}
}

func TestEgressRunArgs_LocksDownProxy(t *testing.T) {
	args := egressRunArgs("yuumi:dev")
	for _, want := range [][]string{
		{"--read-only"},
		{"--cap-drop", "ALL"},
		{"--security-opt", "no-new-privileges"},
		{"--user", "10001:10001"},
		{"--entrypoint", "yuumi-egressproxy", "yuumi:dev"},
	} {
		if !containsSeq(args, want...) {
			t.Errorf("egress run args missing %v: %v", want, args)
		}
	}
	// Proxy nằm ở bridge mặc định (có Internet), được nối thêm vào network
	// sandbox sau — không tự chỉ định --network internal lúc run.
	if slices.Contains(args, "--network") {
		t.Errorf("egress must start on default bridge: %v", args)
	}
}
