package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// sign tạo header "sha256=<hex>" giống cách GitHub ký request thật,
// dùng để build input hợp lệ cho các test case.
func sign(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	secret := "my-secret"
	payload := []byte(`{"action":"created"}`)

	tests := []struct {
		name    string
		secret  string
		payload []byte
		header  string
		want    bool
	}{
		{
			name:    "valid signature",
			secret:  secret,
			payload: payload,
			header:  sign(secret, payload),
			want:    true,
		},
		{
			name:    "wrong secret",
			secret:  "wrong-secret",
			payload: payload,
			header:  sign(secret, payload),
			want:    false,
		},
		{
			name:    "tampered payload",
			secret:  secret,
			payload: []byte(`{"action":"tampered"}`),
			header:  sign(secret, payload),
			want:    false,
		},
		{
			name:    "missing sha256 prefix",
			secret:  secret,
			payload: payload,
			header:  hex.EncodeToString([]byte("no-prefix")),
			want:    false,
		},
		{
			name:    "empty header",
			secret:  secret,
			payload: payload,
			header:  "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifySignature(tt.secret, tt.payload, tt.header)
			if got != tt.want {
				t.Errorf("VerifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPayload_UnmarshalPullRequestEvent đảm bảo Payload parse đúng shape
// thật của webhook event "pull_request" (issue #32) — action, số PR, head
// SHA và tác giả PR, để main.go dùng được ngay không cần parse tay JSON.
func TestPayload_UnmarshalPullRequestEvent(t *testing.T) {
	body := []byte(`{
		"action": "synchronize",
		"repository": {"full_name": "octo/repo"},
		"pull_request": {
			"number": 7,
			"head": {"sha": "abc123"},
			"user": {"login": "author1"}
		},
		"installation": {"id": 555}
	}`)

	var p Payload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if p.Action != "synchronize" {
		t.Errorf("Action = %q, want %q", p.Action, "synchronize")
	}
	if p.Repository.FullName != "octo/repo" {
		t.Errorf("Repository.FullName = %q, want %q", p.Repository.FullName, "octo/repo")
	}
	if p.PullRequest.Number != 7 {
		t.Errorf("PullRequest.Number = %d, want 7", p.PullRequest.Number)
	}
	if p.PullRequest.Head.SHA != "abc123" {
		t.Errorf("PullRequest.Head.SHA = %q, want %q", p.PullRequest.Head.SHA, "abc123")
	}
	if p.PullRequest.User.Login != "author1" {
		t.Errorf("PullRequest.User.Login = %q, want %q", p.PullRequest.User.Login, "author1")
	}
	if p.Installation.ID != 555 {
		t.Errorf("Installation.ID = %d, want 555", p.Installation.ID)
	}
}

// TestPayload_UnmarshalIssueCommentEvent_PullRequestFieldStaysZero đảm bảo
// parse event "issue_comment" hiện có KHÔNG bị ảnh hưởng bởi field
// PullRequest mới thêm — JSON không có "pull_request" thì field này giữ
// nguyên zero value, luồng mention không cần đổi gì.
func TestPayload_UnmarshalIssueCommentEvent_PullRequestFieldStaysZero(t *testing.T) {
	body := []byte(`{
		"action": "created",
		"comment": {"id": 1, "body": "@yuumi-bot review", "user": {"login": "u"}},
		"repository": {"full_name": "octo/repo"},
		"issue": {"number": 3}
	}`)

	var p Payload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if p.Issue.Number != 3 {
		t.Errorf("Issue.Number = %d, want 3", p.Issue.Number)
	}
	if p.PullRequest.Number != 0 || p.PullRequest.Head.SHA != "" {
		t.Errorf("expected zero-value PullRequest field, got %+v", p.PullRequest)
	}
}

func TestPullRequestAutoReviewActions(t *testing.T) {
	for _, action := range []string{"opened", "synchronize"} {
		if !PullRequestAutoReviewActions[action] {
			t.Errorf("expected action %q to trigger auto-review", action)
		}
	}
	for _, action := range []string{"closed", "reopened", "edited", "labeled", "review_requested"} {
		if PullRequestAutoReviewActions[action] {
			t.Errorf("expected action %q NOT to trigger auto-review", action)
		}
	}
}
