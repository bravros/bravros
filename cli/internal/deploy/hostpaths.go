package deploy

import (
	"bytes"
	"path/filepath"
	"strings"
)

// The skills tree is host-agnostic on purpose: tools/skillgen refuses to lint a
// skill containing `~/.claude`, so authors write the toolkit's own home
// (`~/.bravros/...`) or the neutral `~/.agent_config` instead. Neither directory
// holds skills or scripts on a Claude install — everything lives under
// `~/.claude` — so deploying those strings verbatim ships paths that resolve to
// nothing. Every `cp -f ~/.bravros/skills/<x>/scripts/<y>` instruction in a
// deployed skill is then a copy-paste that fails.
//
// De-sanitizing on the way out keeps both ends honest: the repo stays lint-clean
// and portable, and the deployed copy names the directory that actually exists.
//
// Only the subdirectories below are rewritten. `~/.bravros/config.json` and
// `~/.bravros/hooks` are REAL paths — the bravros CLI keeps its own config there
// — so a blanket `~/.bravros/` → `~/.claude/` rewrite would corrupt them. The
// allowlist is deliberate; add to it only for a directory that genuinely lives
// under the agent config dir on a Claude install.
var hostPathRules = []struct{ from, to string }{
	// Longest-first: `~/.agent_config` must be rewritten before any bare
	// `.agent_config/` rule can bite off its tail.
	{"~/.agent_config", "~/.claude"},
	{"~/.bravros/skills", "~/.claude/skills"},
	{"~/.bravros/scripts", "~/.claude/scripts"},
	{"~/.bravros/templates", "~/.claude/templates"},
	{"~/.bravros/bin", "~/.claude/bin"},
	{"~/.bravros/logs", "~/.claude/logs"},
	{"~/.bravros/settings", "~/.claude/settings"},
	{".agent_config/", ".claude/"},
}

// textExtensions are the file types whose contents carry path instructions.
// Anything else is copied byte-for-byte — a rewrite inside a binary would
// corrupt it, and no binary in a skill tree names these paths.
var textExtensions = map[string]bool{
	".md": true, ".markdown": true,
	".yaml": true, ".yml": true,
	".json": true, ".txt": true,
	".sh": true, ".js": true, ".py": true,
}

// isTextFile reports whether a rewrite may be applied to this file's contents.
func isTextFile(name string) bool {
	return textExtensions[strings.ToLower(filepath.Ext(name))]
}

// isClaudeTarget reports whether a deploy target is a Claude config dir, the
// only host where these rewrites are correct. A target named anything else
// (a test temp dir, a future host) is left untouched.
func isClaudeTarget(targetRoot string) bool {
	return filepath.Base(strings.TrimSuffix(targetRoot, string(filepath.Separator))) == ".claude"
}

// desanitizeHostPaths rewrites host-agnostic toolkit paths to their Claude
// equivalents. Returns content unchanged when nothing matches, so the common
// case allocates nothing beyond the input.
func desanitizeHostPaths(content []byte) []byte {
	for _, r := range hostPathRules {
		from := []byte(r.from)
		if !bytes.Contains(content, from) {
			continue
		}
		content = bytes.ReplaceAll(content, from, []byte(r.to))
	}
	return content
}
