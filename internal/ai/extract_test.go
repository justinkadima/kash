package ai

import "testing"

func TestExtractCommands(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "single bash block",
			text: "Try this:\n\n```bash\ngit status\n```\n\nIt shows the state.",
			want: []string{"git status"},
		},
		{
			name: "multiple blocks in order",
			text: "```bash\nls -la\n```\nthen\n```sh\ncd /tmp\n```",
			want: []string{"ls -la", "cd /tmp"},
		},
		{
			name: "unterminated block ignored",
			text: "```bash\ngit stat",
			want: nil,
		},
		{
			name: "unlabeled block accepted",
			text: "```\necho hi\n```",
			want: []string{"echo hi"},
		},
		{
			name: "non-shell block ignored",
			text: "```python\nprint(1)\n```\n```bash\ntrue\n```",
			want: []string{"true"},
		},
		{
			name: "zsh accepted, trimmed",
			text: "```zsh\n\necho x\n\n```",
			want: []string{"echo x"},
		},
		{
			name: "empty block ignored",
			text: "```bash\n\n```",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractCommands(tc.text)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d commands %v, want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				if got[i].Text != tc.want[i] {
					t.Errorf("cmd[%d] = %q, want %q", i, got[i].Text, tc.want[i])
				}
			}
		})
	}
}

func TestIsCommandLang(t *testing.T) {
	for _, lang := range []string{"", "bash", "sh", "zsh", "shell", "console", "SH", " Shell "} {
		if !IsCommandLang(lang) {
			t.Errorf("IsCommandLang(%q) = false, want true", lang)
		}
	}
	for _, lang := range []string{"python", "go", "json", "bashscript"} {
		if IsCommandLang(lang) {
			t.Errorf("IsCommandLang(%q) = true, want false", lang)
		}
	}
}
