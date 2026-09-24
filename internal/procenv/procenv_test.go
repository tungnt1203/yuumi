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
