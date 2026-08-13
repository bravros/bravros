package session

import "os"

// agentEnvVars lists all environment variables that indicate an active agent session.
// When any of these is non-empty, the process is running inside an agent runtime.
// To add support for a new agent, append its session env var here — one place only.
//
// Priority order matters: vars are checked top-to-bottom. CLAUDE_CODE_SESSION_ID
// is the canonical Claude Code session ID env var (added in Claude Code v2.1.132)
// and MUST stay first so it wins over the legacy CLAUDE_SESSION_ID fallback.
//
// This list is a DENY-list for the "am I inside an agent?" safety gate — it is NOT
// a list of supported hosts. Gemini and Antigravity were amputated as skill build /
// deploy targets in P-0183 G6 (they silently emitted Claude-only paths), but their
// session vars stay here on purpose: IsInsideAgentSession() gates `kaisser rule
// disable` and friends, which must refuse to run from inside ANY agent runtime.
// Dropping a runtime from this list would turn it into a bypass terminal for an
// always-on safety gate (audit rule 45). Adding runtimes here is free; removing one
// fails open. Do not "clean these up" alongside a host-support removal.
//
// Primary session ID vars (one per agent runtime):
var agentSessionIDVars = []string{
	"CLAUDE_CODE_SESSION_ID", // canonical, Claude Code v2.1.132+
	"CLAUDE_SESSION_ID",      // legacy fallback (pre-v2.1.132)
	"CODEX_SESSION_ID",
	"CODEX_THREAD_ID",
	"OPENCODE_SESSION_ID",
	"PI_SESSION_ID",
	"PI_CODING_AGENT_SESSION_DIR",
	"GEMINI_SESSION_ID",      // not a supported host — deny-list only (see above)
	"ANTIGRAVITY_SESSION_ID", // not a supported host — deny-list only (see above)
}

// claudeStrongSignals covers additional Claude Code env vars that are always
// present even when CLAUDE_SESSION_ID is not forwarded into subprocesses.
var claudeStrongSignals = []string{
	"CLAUDE_CODE_ENTRYPOINT", // set by Claude Code CLI runtime
	"CLAUDECODE",             // set when running inside Claude Code
}

// codexStrongSignals covers Codex Desktop/CLI env vars that may be present
// even when a dedicated session ID is not forwarded into subprocesses.
var codexStrongSignals = []string{
	"CODEX_SHELL",
	"CODEX_CI",
}

// IsInsideAgentSession returns true if the current process is running inside any
// known agent session (Claude Code, Codex, OpenCode, Pi, Gemini, Antigravity).
//
// This is used as a safety gate: operations that could destabilize running agent
// sessions (e.g. toggling audit rules) must refuse to execute from inside them.
// The user must open a separate terminal to perform those operations.
func IsInsideAgentSession() bool {
	for _, envVar := range agentSessionIDVars {
		if os.Getenv(envVar) != "" {
			return true
		}
	}
	for _, envVar := range claudeStrongSignals {
		if os.Getenv(envVar) != "" {
			return true
		}
	}
	for _, envVar := range codexStrongSignals {
		if os.Getenv(envVar) != "" {
			return true
		}
	}
	return false
}

// Resolve returns the current agent session ID by checking each entry in
// agentSessionIDVars in priority order and returning the first non-empty value.
// Returns "" when no agent session env var is set (i.e. running outside any
// agent runtime).
//
// Priority order (top-to-bottom): CLAUDE_CODE_SESSION_ID (canonical, Claude
// Code v2.1.132+) → CLAUDE_SESSION_ID (legacy fallback) → CODEX_SESSION_ID →
// CODEX_THREAD_ID → OPENCODE_SESSION_ID → PI_SESSION_ID →
// PI_CODING_AGENT_SESSION_DIR → GEMINI_SESSION_ID →
// ANTIGRAVITY_SESSION_ID.
//
// Use Resolve() instead of os.Getenv("CLAUDE_SESSION_ID") at every CLI read site
// so a single helper covers all current and future agent runtimes — and so the
// canonical CLAUDE_CODE_SESSION_ID env var (released in Claude Code v2.1.132) is
// always preferred over the legacy CLAUDE_SESSION_ID. Adding a new agent runtime
// is one slice append in this file.
func Resolve() string {
	for _, envVar := range agentSessionIDVars {
		if v := os.Getenv(envVar); v != "" {
			return v
		}
	}
	return ""
}
