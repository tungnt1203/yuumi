package procenv

import (
	"slices"
	"testing"
)

func TestWithoutSecrets(t *testing.T) {
	env := []string{"PATH=/bin", "GITHUB_WEBHOOK_SECRET=s", "GITHUB_APP_PRIVATE_KEY=k", "ANTHROPIC_API_KEY=a", "GITHUB_APP_ID=1"}
	got := WithoutSecrets(env)
	want := []string{"PATH=/bin", "ANTHROPIC_API_KEY=a", "GITHUB_APP_ID=1"}
	if !slices.Equal(got, want) {
		t.Errorf("WithoutSecrets() = %v, want %v", got, want)
	}
}

func TestOnly(t *testing.T) {
	env := []string{"PATH=/bin", "HOME=/h", "GITHUB_WEBHOOK_SECRET=s", "PATHX=/x"}
	got := Only(env, "PATH", "HOME")
	want := []string{"PATH=/bin", "HOME=/h"}
	if !slices.Equal(got, want) {
		t.Errorf("Only() = %v, want %v", got, want)
	}
}

// Không biến nào khớp vẫn trả slice rỗng khác nil, để cmd.Env không bị hiểu
// là "thừa hưởng toàn bộ env".
func TestOnly_NoMatch_NonNil(t *testing.T) {
	if got := Only([]string{"SECRET=x"}, "PATH"); got == nil || len(got) != 0 {
		t.Errorf("Only() = %#v, want empty non-nil slice", got)
	}
}
