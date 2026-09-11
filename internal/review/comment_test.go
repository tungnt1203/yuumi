package review

import "testing"

func TestMentionsBot(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "mentions bot with command",
			body: "@yuumi-bot review",
			want: true,
		},
		{
			name: "no mention",
			body: "just a normal comment",
			want: false,
		},
		{
			name: "mention is substring, not exact word",
			body: "@yuumi-bot-other review",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Comment{Body: tt.body}
			got := c.MentionsBot("yuumi-bot")
			if got != tt.want {
				t.Errorf("MentionsBot() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractCommand(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{
			name: "mention with one-word command",
			body: "@yuumi-bot review",
			want: "review",
		},
		{
			name: "mention with multi-word command",
			body: "@yuumi-bot review this file please",
			want: "review this file please",
		},
		{
			name: "mention with no command after it",
			body: "@yuumi-bot",
			want: "",
		},
		{
			name:    "no mention at all",
			body:    "just a normal comment",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Comment{Body: tt.body}
			got, err := c.ExtractCommand("yuumi-bot")

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ExtractCommand() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("ExtractCommand() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ExtractCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}
