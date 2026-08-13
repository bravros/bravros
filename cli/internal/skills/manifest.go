package skills

// manifest.go — per-skill dependency manifest aggregator (P-0087).
//
// Schema: each skill's SKILL.md frontmatter may contain a `dependencies:` block:
//
//   dependencies:
//     - name: weasyprint
//       install_cmd_macos: "brew install weasyprint"
//       install_cmd_linux: "sudo apt-get update && sudo apt-get install -y libpango-1.0-0 libpangoft2-1.0-0 && uv tool install weasyprint"
//       check_cmd: "weasyprint --version"
//       optional: false
//
// Prefer a check_cmd that actually INVOKES the tool ("<name> --version") over a
// bare "command -v <name>": a binary can sit on PATH and still be unusable —
// WeasyPrint dlopens GLib/Pango at import, so a uv-installed copy on macOS
// passes "command -v" and fails every render.
//
// `ScanDependencies` walks all skill directories, parses frontmatter, and returns
// a deduplicated list of Dependency entries (keyed by `name`). When two skills
// declare the same dep name, the first occurrence wins (preserving source_skill).

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Dependency describes one installable tool/binary that a skill requires.
type Dependency struct {
	// Name is the canonical dep identifier (e.g. "weasyprint", "wrangler").
	Name string `yaml:"name" json:"name"`

	// InstallCmdMacos is the shell command to install this dep on macOS.
	InstallCmdMacos string `yaml:"install_cmd_macos" json:"install_cmd_macos"`

	// InstallCmdLinux is the shell command to install this dep on Linux.
	InstallCmdLinux string `yaml:"install_cmd_linux" json:"install_cmd_linux"`

	// CheckCmd is a shell expression that exits 0 when the dep is present.
	// Typically "command -v <name>" or a more specific probe.
	CheckCmd string `yaml:"check_cmd" json:"check_cmd"`

	// Optional marks deps that are nice-to-have; install failures are warnings not errors.
	Optional bool `yaml:"optional" json:"optional"`

	// SourceSkill is the skill directory name that first declared this dep (informational).
	SourceSkill string `yaml:"-" json:"source_skill"`
}

// skillFrontmatter is a minimal parse target for SKILL.md YAML.
type skillFrontmatter struct {
	Dependencies []Dependency `yaml:"dependencies"`
}

// ScanDependencies walks skillsDir, parses each skill's SKILL.md frontmatter,
// and returns a deduplicated list of Dependency entries.
// Skills listed in skipSkills are excluded from scanning.
func ScanDependencies(skillsDir string, skipSkills []string) ([]Dependency, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, err
	}

	skipSet := make(map[string]bool, len(skipSkills))
	for _, s := range skipSkills {
		skipSet[s] = true
	}

	seen := make(map[string]bool)
	var deps []Dependency

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillName := e.Name()
		if skipSet[skillName] {
			continue
		}

		skillMDPath := filepath.Join(skillsDir, skillName, "SKILL.md")
		data, err := os.ReadFile(skillMDPath)
		if err != nil {
			continue // no SKILL.md — skip silently
		}

		content := string(data)
		if !strings.HasPrefix(content, "---") {
			continue
		}
		end := strings.Index(content[3:], "---")
		if end < 0 {
			continue
		}
		yamlBlock := content[3 : 3+end]

		var fm skillFrontmatter
		if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
			continue
		}

		for _, dep := range fm.Dependencies {
			dep.Name = strings.TrimSpace(dep.Name)
			if dep.Name == "" {
				continue
			}
			if seen[dep.Name] {
				continue // dedup: first occurrence wins
			}
			seen[dep.Name] = true
			dep.SourceSkill = skillName
			deps = append(deps, dep)
		}
	}

	return deps, nil
}

// DefaultSkipSkills returns the canonical list of skills whose deps are
// intentionally hand-managed and must not appear in the auto-manifest.
func DefaultSkipSkills() []string {
	return []string{
		"yt-search", // PEP 723 inline uv run deps; no external binary needed
	}
}
