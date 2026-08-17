//go:build windows

package managed

// bravrosBin resolves to the D10 Windows install location. %USERPROFILE% is
// left unexpanded (mirrors the unix $HOME convention) — the hook is executed
// by cmd.exe, which expands it per-user, so a literal path would break for
// any other user.
const bravrosBin = `%USERPROFILE%\.claude\bin\bravros.exe`

// guardedCommand emits the bravros sub-command directly, with no desktop-app
// guard. The unix desktopGuard exists because the macOS Claude desktop app is
// a bundle whose SessionStart fires with __CFBundleIdentifier set to
// com.anthropic.* — that condition is macOS-specific and does not exist on
// Windows, so there is nothing to guard against here. A shell `case`
// statement is also not something cmd.exe can parse, so reusing the unix
// form verbatim would simply be broken rather than merely redundant.
func guardedCommand(argv string) string {
	return bravrosBin + " " + argv
}
