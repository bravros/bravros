package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesanitizeHostPaths(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "agent_config skills path",
			in:   "cp -f ~/.agent_config/skills/batch-merge-prs/scripts/verify-prs.js .claude/workflows/",
			want: "cp -f ~/.claude/skills/batch-merge-prs/scripts/verify-prs.js .claude/workflows/",
		},
		{
			name: "bravros skills path",
			in:   "uv run ~/.bravros/skills/graphify-status/scripts/graphify-status.py",
			want: "uv run ~/.claude/skills/graphify-status/scripts/graphify-status.py",
		},
		{
			name: "bravros scripts path",
			in:   "cp ~/.bravros/scripts/graphify-refresh-hook.sh scripts/",
			want: "cp ~/.claude/scripts/graphify-refresh-hook.sh scripts/",
		},
		{
			name: "bare agent_config dir reference",
			in:   "latest version from .agent_config/templates.",
			want: "latest version from .claude/templates.",
		},
		{
			// The CLI keeps its own config under ~/.bravros — rewriting it would
			// point the toolkit at a file that does not exist. Not in the allowlist.
			name: "bravros config is NOT rewritten",
			in:   "settings live in ~/.bravros/config.json and ~/.bravros/hooks",
			want: "settings live in ~/.bravros/config.json and ~/.bravros/hooks",
		},
		{
			name: "no tokens is a passthrough",
			in:   "nothing to rewrite here",
			want: "nothing to rewrite here",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(desanitizeHostPaths([]byte(tc.in))); got != tc.want {
				t.Errorf("desanitizeHostPaths()\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

func TestIsClaudeTarget(t *testing.T) {
	cases := map[string]bool{
		"/Users/x/.claude":  true,
		"/Users/x/.claude/": true,
		"/Users/x/.bravros": false,
		"/tmp/deploy-test":  false,
	}
	for in, want := range cases {
		if got := isClaudeTarget(in); got != want {
			t.Errorf("isClaudeTarget(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsTextFile(t *testing.T) {
	cases := map[string]bool{
		"SKILL.md": true, "skill.yaml": true, "a.yml": true,
		"x.sh": true, "x.js": true, "x.py": true, "x.json": true, "x.txt": true,
		"logo.png": false, "binary": false, "a.pyc": false,
	}
	for in, want := range cases {
		if got := isTextFile(in); got != want {
			t.Errorf("isTextFile(%q) = %v, want %v", in, got, want)
		}
	}
}

// copySkillDir must rewrite text files but leave binaries byte-identical, and
// must not rewrite at all when the target is not a Claude config dir.
func TestCopySkillDirRewrite(t *testing.T) {
	binary := []byte{0x00, 0x01, '~', '/', '.', 'b', 'r', 'a', 'v', 'r', 'o', 's', '/', 's', 'k', 'i', 'l', 'l', 's', 0xff}

	src := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("run ~/.bravros/skills/x/y.py\n"), 0644))
	must(os.WriteFile(filepath.Join(src, "logo.png"), binary, 0644))

	t.Run("rewrites text, preserves binary", func(t *testing.T) {
		dst := t.TempDir()
		must(copySkillDir(src, dst, true))

		got, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
		must(err)
		if want := "run ~/.claude/skills/x/y.py\n"; string(got) != want {
			t.Errorf("SKILL.md = %q, want %q", got, want)
		}

		gotBin, err := os.ReadFile(filepath.Join(dst, "logo.png"))
		must(err)
		if string(gotBin) != string(binary) {
			t.Errorf("binary was modified: %v", gotBin)
		}
	})

	t.Run("rewrite disabled leaves content untouched", func(t *testing.T) {
		dst := t.TempDir()
		must(copySkillDir(src, dst, false))

		got, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
		must(err)
		if want := "run ~/.bravros/skills/x/y.py\n"; string(got) != want {
			t.Errorf("SKILL.md = %q, want %q", got, want)
		}
	})
}
