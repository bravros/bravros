package main

import (
	"testing"
)

func TestLintSkill(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name:    "clean content",
			content: "This is a host-neutral skill instructions.",
			wantErr: false,
		},
		{
			name:    "contains AskUserQuestion",
			content: "Please AskUserQuestion to confirm.",
			wantErr: true,
		},
		{
			name:    "contains Agent(",
			content: "Call Agent(team) to process.",
			wantErr: true,
		},
		{
			name:    "contains mcp__",
			content: "Use tool mcp__fetch_url to read.",
			wantErr: true,
		},
		{
			name:    "contains ~/.claude",
			content: "Read from ~/.claude/skills/ directory.",
			wantErr: true,
		},
		{
			name:    "contains kaisser",
			content: "Always run kaisser commit to commit.",
			wantErr: true,
		},
		{
			name:    "contains Kaisser case-insensitive",
			content: "This is a Kaisser tool.",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := LintSkill("test.md", tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("LintSkill() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "replaces Claude Code",
			content: "Always use Claude Code to run.",
			want:    "Always use the AI CLI to run.",
		},
		{
			name:    "replaces CLAUDE.md",
			content: "Write to CLAUDE.md file.",
			want:    "Write to AGENT.md file.",
		},
		{
			name:    "preserves code blocks",
			content: "Use it:\n```\nClaude Code settings.json\n```\nOutside: Claude Code",
			want:    "Use it:\n```\nClaude Code settings.json\n```\nOutside: the AI CLI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Sanitize(tt.content)
			if got != tt.want {
				t.Errorf("Sanitize() got = %q, want = %q", got, tt.want)
			}
		})
	}
}
