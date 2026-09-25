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
	inspect string // stdout của `network inspect`; "" kèm inspectErr = chưa có network
	errs    map[string]error
}

func (f *fakeDocker) run(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	key := strings.Join(args[:min(2, len(args))], " ")
	if err := f.errs[key]; err != nil {
		return "", err
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

func TestSetupNetwork_CreatesInternalNetworkAndEgress(t *testing.T) {
	f := &fakeDocker{}
	if err := setupNetwork(f.run, "yuumi:dev"); err != nil {
		t.Fatalf("setupNetwork() error = %v", err)
	}
	if !f.called("network", "create", "--internal", "yuumi-sandbox") {
		t.Errorf("network not created with --internal: %v", f.calls)
	}
	if !f.called("rm", "--force", "yuumi-egress") {
		t.Errorf("old egress container not removed: %v", f.calls)
	}
	if !f.called("run", "--detach", "--name", "yuumi-egress") {
		t.Errorf("egress container not started: %v", f.calls)
	}
	if !f.called("network", "connect", "yuumi-sandbox", "yuumi-egress") {
		t.Errorf("egress not connected to sandbox network: %v", f.calls)
	}
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
	for _, want := range [][]string{
		{"rm", "--force", "yuumi-egress"},
		{"run", "--detach", "--name", "yuumi-egress"},
		{"network", "connect", "yuumi-sandbox", "yuumi-egress"},
	} {
		if !f.called(want...) {
			t.Errorf("missing %v when network already exists: %v", want, f.calls)
		}
	}
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
	for _, step := range []string{"network create", "rm --force", "run --detach", "network connect"} {
		t.Run(step, func(t *testing.T) {
			f := &fakeDocker{errs: map[string]error{step: errors.New("daemon down")}}
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
