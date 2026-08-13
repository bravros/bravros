package secrets

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Marker constants. The v2 block is a single fenced region delimited by exact
// full-line markers. Migration strips both the v2 block (on re-runs) and the
// legacy v1 block + eager `export … =$(op read …)` lines (one-time).
const (
	markerV2Start = "# >>> bravros secrets (v2) >>>"
	markerV2End   = "# <<< bravros secrets (v2) <<<"
	markerV1Start = "# --- bravros secrets bootstrap ---"
	markerV1End   = "# --- end bravros secrets bootstrap ---"
)

// eagerOpExportRe matches the legacy eager export lines this migration removes,
// e.g. `export FIRECRAWL_API_KEY=$(op read 'op://…')` (any whitespace, any
// quoting). Only the two managed vars are targeted — we never touch unrelated
// user op-read lines.
var eagerOpExportRe = regexp.MustCompile(`^\s*export\s+(FIRECRAWL_API_KEY|HASS_TOKEN)\s*=\s*\$\(\s*op\s+read\b`)

// BootstrapOpts configures the secrets bootstrap operation.
type BootstrapOpts struct {
	ShellRC   string    // RC file to rewrite (default: ~/.zshrc)
	Backend   Backend   // empty → DetectBackend(); else forces op|env|none
	DryRun    bool      // print the rendered block to stdout, do not touch the rc
	PrintOnly bool      // print the rendered block to stdout (alias of DryRun for the block)
	Output    io.Writer // for testing — if nil, uses os.Stdout
}

// BootstrapResult holds the outcome of the bootstrap.
type BootstrapResult struct {
	Backend Backend  `json:"backend"`
	Written []string `json:"written,omitempty"`
	RCFile  string   `json:"rc_file,omitempty"`
	DryRun  bool     `json:"dry_run,omitempty"`
}

// Bootstrap rewrites the v2 secrets block in the target shell RC file for the
// selected backend.  It is idempotent: running twice with the same backend
// yields a byte-identical rc with exactly one v2 block. It NEVER truncates the
// live rc in place — all rewriting goes through writeRCAtomic (CreateTemp +
// os.Rename), so a crash mid-write leaves the original intact.
func Bootstrap(opts BootstrapOpts) (*BootstrapResult, error) {
	backend := opts.Backend
	if backend == "" {
		backend = DetectBackend()
	}
	result := &BootstrapResult{Backend: backend, DryRun: opts.DryRun}

	block := RenderBlock(backend)
	for _, s := range Registry() {
		result.Written = append(result.Written, s.EnvVar)
	}

	// Print-only / dry-run: emit the rendered block, never touch the rc.
	if opts.PrintOnly || opts.DryRun {
		out := opts.Output
		if out == nil {
			out = os.Stdout
		}
		fmt.Fprint(out, block)
		return result, nil
	}

	// Determine target RC file.
	rcFile := opts.ShellRC
	if rcFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cannot determine home dir: %w", err)
		}
		rcFile = filepath.Join(home, ".zshrc")
	}
	result.RCFile = rcFile

	if err := rewriteRC(rcFile, block); err != nil {
		return nil, err
	}
	return result, nil
}

// rewriteRC reads the existing rc, strips the old v2 block + legacy v1 block +
// eager op-export lines, appends the fresh block, and writes the result
// atomically. A missing rc is treated as empty (the block is created fresh).
func rewriteRC(rcFile, block string) error {
	var existing string
	if data, err := os.ReadFile(rcFile); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot read %s: %w", rcFile, err)
	}

	cleaned := stripManaged(existing)

	// Compose final content: cleaned body + exactly one trailing newline gap +
	// the fresh block. RenderBlock already ends with a newline.
	var sb strings.Builder
	cleaned = strings.TrimRight(cleaned, "\n")
	if cleaned != "" {
		sb.WriteString(cleaned)
		sb.WriteString("\n\n")
	}
	sb.WriteString(block)

	return writeRCAtomic(rcFile, sb.String())
}

// stripManaged removes (a) any bravros v2 block, (b) any legacy v1 block, and
// (c) any eager `export (FIRECRAWL_API_KEY|HASS_TOKEN)=$(op read …)` line, from
// the rc text. Marker matching is EXACT on the trimmed full line. A start marker
// with no matching end marker stops at EOF — it NEVER deletes past the file end
// or eats unrelated trailing content beyond what the block could contain.
func stripManaged(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))

	inBlock := false      // currently inside a v1 or v2 managed block
	var endMarker string  // the exact end marker we are scanning for

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if inBlock {
			// Inside a managed block: drop every line until (and including) the
			// matching end marker. Missing end marker → consume to EOF.
			if trimmed == endMarker {
				inBlock = false
				endMarker = ""
			}
			continue
		}

		// Block starts — exact trimmed-equal match on a start marker.
		if trimmed == markerV2Start {
			inBlock = true
			endMarker = markerV2End
			continue
		}
		if trimmed == markerV1Start {
			inBlock = true
			endMarker = markerV1End
			continue
		}

		// Standalone legacy eager op-export line (outside any block).
		if eagerOpExportRe.MatchString(line) {
			continue
		}

		out = append(out, line)
	}

	return strings.Join(out, "\n")
}

// RenderBlock returns the full v2 fenced block for the given backend. The block
// ALWAYS defines bravros_secret() FIRST (before any call or sourcing the rc
// errors out), then emits per-backend gate lines, then exports the backend
// marker. The `none` backend emits ZERO op references.
func RenderBlock(backend Backend) string {
	var b strings.Builder
	b.WriteString(markerV2Start + "\n")
	b.WriteString("# Managed by `bravros secrets`. Do not edit between these markers —\n")
	b.WriteString("# changes are overwritten on the next `bravros secrets bootstrap`.\n")
	b.WriteString("# Backend: " + string(backend) + "\n")

	// bravros_secret() MUST be defined first. It is POSIX-sh safe (works in both
	// bash and zsh) and delegates the actual resolution to the bravros binary,
	// which honors BRAVROS_SECRETS_BACKEND / BRAVROS_ENV_FILE precedence. If the
	// bravros binary is absent the function fast-fails to empty (silent), so a
	// missing CLI never breaks shell startup.
	b.WriteString(bravrosSecretFn)

	switch backend {
	case BackendOp:
		b.WriteString("export " + backendEnvVar + "=op\n")
		// Gate lines: only run when op is installed AND a live session exists.
		// `op whoami` returns in ms and never pops a biometric prompt, so it is
		// safe at shell startup. On a no-session machine the whole branch is a
		// no-op and the vars stay unset (features degrade gracefully).
		b.WriteString("if command -v op >/dev/null 2>&1 && op whoami >/dev/null 2>&1; then\n")
		for _, s := range Registry() {
			b.WriteString("  " + opGateLine(s) + "\n")
		}
		b.WriteString("fi\n")

	case BackendEnv:
		b.WriteString("export " + backendEnvVar + "=env\n")
		// Gate lines resolve from $BRAVROS_ENV_FILE via bravros_secret. The op
		// branch is guarded OFF — even if op is installed, the env backend never
		// shells out to 1Password.
		for _, s := range Registry() {
			b.WriteString(envGateLine(s) + "\n")
		}

	case BackendKeychain:
		// macOS-only, opt-in. ZERO op references — keychain resolution happens
		// entirely inside the bravros binary via `security find-generic-password`,
		// so the shell only needs to set the backend marker (darwin-guarded) and
		// emit the same bravros_secret gate lines. On a non-darwin host the marker
		// is never exported, so DetectBackend() falls back to its auto-detect path
		// (op/env) instead of a keychain it cannot read.
		b.WriteString("if [ \"$(uname -s)\" = \"Darwin\" ]; then\n")
		b.WriteString("  export " + backendEnvVar + "=keychain\n")
		for _, s := range Registry() {
			b.WriteString("  " + keychainGateLine(s) + "\n")
		}
		b.WriteString("fi\n")

	case BackendNone:
		// ZERO op references. No gate lines, no bravros_secret calls — just the
		// backend marker so Resolve()/DetectBackend() see `none`.
		b.WriteString("export " + backendEnvVar + "=none\n")
	}

	b.WriteString(markerV2End + "\n")
	return b.String()
}

// bravrosSecretFn is the shell function definition emitted once, first, in every
// v2 block (except it is still emitted for `none` so the contract "function is
// always defined" holds — sourcing a downstream script that calls
// bravros_secret won't error). It is intentionally POSIX-sh compatible.
const bravrosSecretFn = `bravros_secret() {
  # bravros_secret ENVVAR OP_ITEM OP_FIELD
  # Resolves a managed secret via the bravros binary (honors
  # BRAVROS_SECRETS_BACKEND / BRAVROS_ENV_FILE). Self-gating: if the value is
  # already exported it is left untouched; if the bravros binary is missing it
  # fast-fails to empty without erroring.
  _ks_var="$1"
  # Read the current (exported) value indirectly WITHOUT eval — printenv takes
  # the variable NAME as data, so a name with shell metacharacters can never be
  # executed. POSIX-portable (printenv ships on macOS + Linux); if it is somehow
  # absent the substitution is empty and we simply fall through to resolution.
  _ks_cur="$(printenv "$_ks_var" 2>/dev/null)"
  [ -n "$_ks_cur" ] && return 0
  command -v bravros >/dev/null 2>&1 || return 0
  _ks_val="$(bravros secrets resolve "$1" "$2" "$3" 2>/dev/null)"
  [ -n "$_ks_val" ] && export "$_ks_var=$_ks_val"
  unset _ks_var _ks_cur _ks_val
}
`

// opGateLine renders a single op-backend resolution call. The whole op branch is
// already guarded by `op whoami`, but bravros_secret re-honors the backend so
// the binary stays the single source of precedence truth.
func opGateLine(s KnownSecret) string {
	return fmt.Sprintf("bravros_secret %s %s %s",
		shellQuote(s.EnvVar), shellQuote(s.OPItem), shellQuote(s.OPField))
}

// envGateLine renders a single env-backend resolution call. Under the env
// backend the bravros binary resolves from $BRAVROS_ENV_FILE only — the op
// branch inside Resolve() is guarded off — so this never reaches 1Password.
func envGateLine(s KnownSecret) string {
	return fmt.Sprintf("bravros_secret %s %s %s",
		shellQuote(s.EnvVar), shellQuote(s.OPItem), shellQuote(s.OPField))
}

// keychainGateLine renders a single keychain-backend resolution call. The
// bravros binary re-honors BRAVROS_SECRETS_BACKEND=keychain and reads from the
// login keychain via `security` — so this line carries ZERO op references. The
// op-item args are still passed (bravros_secret takes a fixed 3-arg shape) but
// the keychain Resolve branch ignores them in favor of the registry's
// service/account mapping.
func keychainGateLine(s KnownSecret) string {
	return fmt.Sprintf("bravros_secret %s %s %s",
		shellQuote(s.EnvVar), shellQuote(s.OPItem), shellQuote(s.OPField))
}

// shellQuote single-quotes an argument for safe POSIX-sh embedding. Managed
// values are simple identifiers / vault-item names, but we quote defensively so
// a space or special char in an OP item name (e.g. "Firecrawl API") can never
// split into two shell words.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// writeRCAtomic writes content to path atomically: it creates a temp file in the
// SAME directory (so os.Rename is a same-filesystem atomic move), writes the
// full content, fsyncs, then renames over the target. On ANY failure the
// original rc is left byte-for-byte intact and the temp file is cleaned up — we
// NEVER os.Truncate the live rc and NEVER shell out to `sed -i`.
func writeRCAtomic(path, content string) error {
	dir := filepath.Dir(path)
	// Preserve the existing file mode if the rc already exists; else 0644.
	mode := os.FileMode(0644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, ".bravros-rc-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail before a successful rename.
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if _, err := tmp.WriteString(content); err != nil {
		cleanup()
		return fmt.Errorf("cannot write temp rc: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("cannot sync temp rc: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		cleanup()
		return fmt.Errorf("cannot chmod temp rc: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("cannot close temp rc: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("cannot rename temp rc over %s: %w", path, err)
	}
	return nil
}
