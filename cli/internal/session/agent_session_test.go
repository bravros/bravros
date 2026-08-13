package session

import "testing"

// allAgentVars returns all env vars checked by IsInsideAgentSession for cleanup.
func allAgentVars() []string {
	all := make([]string, 0, len(agentSessionIDVars)+len(claudeStrongSignals)+len(codexStrongSignals))
	all = append(all, agentSessionIDVars...)
	all = append(all, claudeStrongSignals...)
	all = append(all, codexStrongSignals...)
	return all
}

func TestIsInsideAgentSession(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    bool
	}{
		{
			name:    "no env vars set",
			envVars: map[string]string{},
			want:    false,
		},
		{
			name:    "CLAUDE_CODE_SESSION_ID set (canonical, v2.1.132+)",
			envVars: map[string]string{"CLAUDE_CODE_SESSION_ID": "abc123"},
			want:    true,
		},
		{
			name:    "CLAUDE_SESSION_ID set",
			envVars: map[string]string{"CLAUDE_SESSION_ID": "abc123"},
			want:    true,
		},
		{
			name:    "CODEX_SESSION_ID set",
			envVars: map[string]string{"CODEX_SESSION_ID": "abc123"},
			want:    true,
		},
		{
			name:    "CODEX_THREAD_ID set",
			envVars: map[string]string{"CODEX_THREAD_ID": "abc123"},
			want:    true,
		},
		{
			name:    "OPENCODE_SESSION_ID set",
			envVars: map[string]string{"OPENCODE_SESSION_ID": "abc123"},
			want:    true,
		},
		{
			name:    "PI_SESSION_ID set",
			envVars: map[string]string{"PI_SESSION_ID": "abc123"},
			want:    true,
		},
		{
			name:    "PI_CODING_AGENT_SESSION_DIR set",
			envVars: map[string]string{"PI_CODING_AGENT_SESSION_DIR": "/tmp/pi-session"},
			want:    true,
		},
		{
			name:    "GEMINI_SESSION_ID set",
			envVars: map[string]string{"GEMINI_SESSION_ID": "abc123"},
			want:    true,
		},
		{
			name:    "ANTIGRAVITY_SESSION_ID set",
			envVars: map[string]string{"ANTIGRAVITY_SESSION_ID": "abc123"},
			want:    true,
		},
		{
			name: "all five agent session ID env vars set",
			envVars: map[string]string{
				"CLAUDE_SESSION_ID":      "abc",
				"CODEX_SESSION_ID":       "def",
				"CODEX_THREAD_ID":        "thread",
				"OPENCODE_SESSION_ID":    "ghi",
				"PI_SESSION_ID":          "pi",
				"GEMINI_SESSION_ID":      "jkl",
				"ANTIGRAVITY_SESSION_ID": "mno",
			},
			want: true,
		},
		{
			name:    "CLAUDE_CODE_ENTRYPOINT set (Claude Code strong signal)",
			envVars: map[string]string{"CLAUDE_CODE_ENTRYPOINT": "cli"},
			want:    true,
		},
		{
			name:    "CLAUDECODE set (Claude Code strong signal)",
			envVars: map[string]string{"CLAUDECODE": "1"},
			want:    true,
		},
		{
			name:    "CODEX_SHELL set (Codex strong signal)",
			envVars: map[string]string{"CODEX_SHELL": "1"},
			want:    true,
		},
		{
			name:    "CODEX_CI set (Codex strong signal)",
			envVars: map[string]string{"CODEX_CI": "1"},
			want:    true,
		},
		{
			name:    "empty string is not set",
			envVars: map[string]string{"CLAUDE_SESSION_ID": ""},
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Clear all agent env vars before each sub-test.
			for _, v := range allAgentVars() {
				t.Setenv(v, "")
			}
			// Set the vars for this test case.
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}
			got := IsInsideAgentSession()
			if got != tc.want {
				t.Errorf("IsInsideAgentSession() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolve_PrefersClaudeCodeSessionID verifies that when both env vars are
// set, Resolve() returns the canonical CLAUDE_CODE_SESSION_ID value (priority
// order matters — v2.1.132 entry must win over the legacy var).
func TestResolve_PrefersClaudeCodeSessionID(t *testing.T) {
	for _, v := range allAgentVars() {
		t.Setenv(v, "")
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "canonical-id")
	t.Setenv("CLAUDE_SESSION_ID", "legacy-id")

	got := Resolve()
	if got != "canonical-id" {
		t.Errorf("Resolve() = %q, want %q (canonical CLAUDE_CODE_SESSION_ID must win)", got, "canonical-id")
	}
}

// TestResolve_FallsBackToLegacy verifies that when only the legacy
// CLAUDE_SESSION_ID is set, Resolve() returns its value (back-compat).
func TestResolve_FallsBackToLegacy(t *testing.T) {
	for _, v := range allAgentVars() {
		t.Setenv(v, "")
	}
	t.Setenv("CLAUDE_SESSION_ID", "legacy-id")

	got := Resolve()
	if got != "legacy-id" {
		t.Errorf("Resolve() = %q, want %q (legacy fallback must work)", got, "legacy-id")
	}
}

// TestResolve_EmptyOutsideSession verifies Resolve() returns "" when no agent
// session env var is set (e.g. running outside Claude Code).
func TestResolve_EmptyOutsideSession(t *testing.T) {
	for _, v := range allAgentVars() {
		t.Setenv(v, "")
	}

	got := Resolve()
	if got != "" {
		t.Errorf("Resolve() = %q, want empty string (no session env var set)", got)
	}
}

// TestResolve_RespectsAgentPriority verifies that for non-Claude agent runtimes
// (Codex, OpenCode, Gemini, Antigravity), Resolve() still returns the correct
// value when those vars are the only ones set.
func TestResolve_RespectsAgentPriority(t *testing.T) {
	for _, v := range allAgentVars() {
		t.Setenv(v, "")
	}
	t.Setenv("CODEX_SESSION_ID", "codex-id")

	got := Resolve()
	if got != "codex-id" {
		t.Errorf("Resolve() = %q, want %q (Codex agent ID should resolve)", got, "codex-id")
	}
}

// TestResolve_UsesCodexThreadID verifies Codex Desktop's thread ID is treated
// as the Codex session identity when CODEX_SESSION_ID is absent.
func TestResolve_UsesCodexThreadID(t *testing.T) {
	for _, v := range allAgentVars() {
		t.Setenv(v, "")
	}
	t.Setenv("CODEX_THREAD_ID", "codex-thread-id")

	got := Resolve()
	if got != "codex-thread-id" {
		t.Errorf("Resolve() = %q, want %q (Codex thread ID should resolve)", got, "codex-thread-id")
	}
}
