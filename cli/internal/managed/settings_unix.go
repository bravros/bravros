//go:build !windows

package managed

// bravrosBin is intentionally an unexpanded $HOME reference: the hook is
// executed by a shell, and a literal path would break for any other user.
const bravrosBin = "$HOME/.claude/bin/bravros"

// desktopGuard makes the hook a no-op inside the Claude desktop app, whose
// SessionStart fires with __CFBundleIdentifier set to com.anthropic.*.
// Load-bearing: without it the selfupdate hook runs inside the app. This
// guard is macOS-specific (the desktop app is a Mac bundle), but it is kept
// on the whole !windows build (i.e. also linux) rather than narrowed further
// — that mirrors the pre-split behavior byte-for-byte, which is required so
// the emitted settings.json does not change on darwin/linux.
const desktopGuard = `case "$__CFBundleIdentifier" in com.anthropic.*) exit 0;; esac; `

// guardedCommand wraps a bravros sub-command in the desktop-app guard.
// Output is byte-identical to the pre-split implementation.
func guardedCommand(argv string) string {
	return "sh -c '" + desktopGuard + "exec " + bravrosBin + " " + argv + "'"
}
