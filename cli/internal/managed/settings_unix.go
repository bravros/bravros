//go:build !windows

package managed

// bravrosBin is intentionally an unexpanded $HOME reference: the hook is
// executed by a shell, and a literal path would break for any other user.
const bravrosBin = "$HOME/.claude/bin/bravros"

// desktopGuard makes the hook a no-op inside the Claude desktop app, whose
// SessionStart fires with __CFBundleIdentifier set to the app's bundle id.
// Load-bearing: without it the selfupdate hook runs inside the app.
//
// The pattern matches ONE bundle id, not the com.anthropic.* prefix it used
// to. Claude Code ships its own Mac bundle — com.anthropic.claude-code, at
// ~/Library/Application Support/Claude/claude-code/<v>/claude.app — and the
// URL handler another (com.anthropic.claude-code-url-handler). A prefix
// glob therefore silently disabled every bravros hook whenever Claude Code
// was launched from its bundle rather than a terminal, the police merge gate
// included. Add a bundle id here only after checking it is not one of ours.
//
// macOS-specific (the desktop app is a Mac bundle) but kept on the whole
// !windows build, i.e. also linux, where __CFBundleIdentifier is never set
// and the case simply never matches.
const desktopGuard = `case "$__CFBundleIdentifier" in com.anthropic.claudefordesktop) exit 0;; esac; `

// guardedCommand wraps a bravros sub-command in the desktop-app guard.
// Output is byte-identical to the pre-split implementation.
func guardedCommand(argv string) string {
	return "sh -c '" + desktopGuard + "exec " + bravrosBin + " " + argv + "'"
}
