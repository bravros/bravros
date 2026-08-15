package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bravros/bravros/cli/internal/config"
	"github.com/bravros/bravros/cli/internal/deploy"
	"github.com/bravros/bravros/cli/internal/managed"
	"github.com/bravros/bravros/cli/internal/skillhygiene"
	"github.com/spf13/cobra"
)

// skillHygieneViolation pairs a SKILL.md path with its detected unsafe iteration matches.
type skillHygieneViolation struct {
	path    string
	matches []skillhygiene.Match
}

// preDeployBashHygieneLint scans source SKILL.md files for unsafe `for X in $VAR`
// bash iteration patterns (the zsh word-split footgun documented in Rule 50).
// Returns a non-nil error listing all violations when the source tree contains
// any unsafe pattern. When force=true the violations are printed as warnings
// to stderr and no error is returned (the deploy proceeds).
//
// sourceDir defaults to cwd when empty, mirroring deploy.Deploy's resolution.
// w is the warning sink (default os.Stderr).
func preDeployBashHygieneLint(sourceDir string, force bool, w io.Writer) error {
	if w == nil {
		w = os.Stderr
	}
	if sourceDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("pre-deploy lint: cannot determine cwd: %w", err)
		}
		sourceDir = cwd
	}

	skillsRoot := filepath.Join(sourceDir, "skills")
	if _, err := os.Stat(skillsRoot); err != nil {
		if os.IsNotExist(err) {
			// No skills dir → nothing to lint; deploy proceeds (other dirs may still copy).
			return nil
		}
		// Other stat errors (permissions, I/O) must not silently disable the lint —
		// surface the failure so the operator sees the broken precondition.
		return fmt.Errorf("pre-deploy lint: stat %s: %w", skillsRoot, err)
	}

	var violations []skillHygieneViolation

	walkErr := filepath.WalkDir(skillsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "SKILL.md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		matches := skillhygiene.Detect(string(data))
		if len(matches) > 0 {
			rel, relErr := filepath.Rel(sourceDir, path)
			if relErr != nil {
				rel = path
			}
			violations = append(violations, skillHygieneViolation{path: rel, matches: matches})
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("pre-deploy lint walk: %w", walkErr)
	}

	if len(violations) == 0 {
		return nil
	}

	totalMatches := 0
	for _, v := range violations {
		totalMatches += len(v.matches)
	}

	var b strings.Builder
	prefix := "❌ deploy blocked"
	if force {
		prefix = "⚠️  --force: skill bash hygiene warning"
	}
	b.WriteString(prefix + " — skill bash hygiene violation(s):\n")
	for _, v := range violations {
		for _, m := range v.matches {
			b.WriteString(fmt.Sprintf("  %s:%d %s\n", v.path, m.Line, m.Snippet))
		}
	}
	if !force {
		b.WriteString("\nzsh (the Bash tool's runtime on macOS) does NOT word-split bare $VAR in for-loops.\n")
		b.WriteString("Use portable array form:\n")
		b.WriteString("  mapfile -t ARR < <(echo \"$LIST\" | tr ' ' '\\n' | grep -v '^$')\n")
		b.WriteString("  for x in \"${ARR[@]}\"; do ...; done\n\n")
		b.WriteString("See skills/CLAUDE.md#bash-hygiene for the canonical recipe.\n")
		b.WriteString("Re-run with --force to downgrade this to a warning and deploy anyway.\n")
		fmt.Fprint(w, b.String())
		return fmt.Errorf("skill bash hygiene lint failed: %d violation(s)", totalMatches)
	}
	fmt.Fprint(w, b.String())
	return nil
}

var (
	deployDryRun    bool
	deployCountOnly bool
	deployField     string
	deployForce     bool
	deployNoPrune   bool
	deployJSON      bool   // emit the full DeployResult JSON object instead of the human summary
	deployFilter    string // comma-separated skill names; overrides .bravros.yml:skills.enabled
	deploySource    string // --source: deploy from an explicit dir instead of cwd (e.g. a selfupdate-fetched payload)
)

// printDeploySummary writes a concise, human-readable deploy summary to w.
// This is the default `bravros deploy` output — the full DeployResult JSON
// object is available via --json (and a single field via --field).
func printDeploySummary(r *deploy.DeployResult, w io.Writer) {
	verb, pruneVerb := "Deployed", "Pruned"
	if r.DryRun {
		verb, pruneVerb = "Would deploy", "Would prune"
	}
	skills := len(r.SkillsDeployed) + len(r.SkillsSkipped)
	fmt.Fprintf(w, "%s: %d skill(s) (%d updated, %d unchanged), %d file(s)\n",
		verb, skills, len(r.SkillsDeployed), len(r.SkillsSkipped), r.FilesDeployed)
	if len(r.Pruned) > 0 {
		fmt.Fprintf(w, "%s %d orphan(s): %s\n", pruneVerb, len(r.Pruned), strings.Join(r.Pruned, ", "))
	}
}

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy the toolkit runtime into the host config dir",
	Long: `Copy skills, hooks, templates, config/settings.json, config/statusline.sh,
and CLAUDE.md from the source repo to ~/.claude/.
Skips mcp.json (machine-specific) and scripts/ (empty).

Behavior:
  - Broken-symlink resilience: before copying, any broken symlink found under
    skills/, hooks/, or templates/ in the destination is automatically removed.
    This prevents ENOENT errors when an old installer left symlinks pointing to
    a shared/ target that no longer exists. Runs unconditionally — --force is
    not required.
  - Orphan pruning: deployed entries with no source counterpart are removed by
    default (--no-prune disables this).
  - Incremental copy: files already up to date (mtime + size match) are
    skipped unless --force is set.
  - Skill allowlist: when .bravros.yml contains skills.enabled: [name1, name2],
    only the listed skills and any skill with "core: true" in SKILL.md frontmatter
    are deployed. Use --filter to override per-invocation without editing config.

Output: prints a concise human-readable summary by default. Use --json for the
full DeployResult object (deployed files, skipped, pruned, skill lists) or
--field <name> to extract a single value.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Resolve enabled-skills allowlist: --filter flag overrides .bravros.yml:skills.enabled.
		enabledSkills := config.EnabledSkills()
		filterMode := false
		if deployFilter != "" {
			parts := strings.Split(deployFilter, ",")
			enabledSkills = make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					enabledSkills = append(enabledSkills, p)
				}
			}
			// --filter is an additive per-invocation override: non-filtered skills
			// already on disk must never be pruned (FilterMode preserves them).
			filterMode = true
		}

		opts := deploy.DeployOpts{
			DryRun:         deployDryRun,
			CountOnly:      deployCountOnly,
			Force:          deployForce,
			NoPrune:        deployNoPrune,
			SourceDir:      deploySource,
			PreserveSkills: config.PreservedSkills(),
			EnabledSkills:  enabledSkills,
			FilterMode:     filterMode,
		}

		// Pre-deploy bash hygiene lint (Rule 50 mirror).
		// Refuses by default when any source SKILL.md contains an unsafe
		// `for X in $VAR` zsh word-split pattern. --force downgrades to a
		// warning so emergency deploys can proceed with the violation logged.
		//
		// Must lint the SAME resolved source dir the deploy itself will use —
		// deploySource when --source was passed, cwd otherwise (mirroring
		// deploy.Deploy's own SourceDir default). Linting cwd while deploying
		// from an explicit --source would silently skip the gate.
		if err := preDeployBashHygieneLint(deploySource, deployForce, os.Stderr); err != nil {
			os.Exit(1)
		}

		result, err := deploy.Deploy(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Deploying the runtime is worthless if nothing ever invokes it, so the
		// same step registers the bravros SessionStart hooks + statusline in the
		// global settings.json. Entry-level merge — user-owned settings survive.
		// (Never done from `bravros init`: that command is repo-local.)
		if !deployDryRun && !deployCountOnly {
			settingsPath := filepath.Join(config.ConfigDir(), "settings.json")
			sync, syncErr := managed.SyncClaudeSettings(settingsPath)
			switch {
			case syncErr != nil:
				fmt.Fprintf(os.Stderr, "Error: settings.json: %v\n", syncErr)
				os.Exit(1)
			case sync.Changed:
				fmt.Fprintf(os.Stderr, "  → registered bravros hooks + statusline in %s\n", settingsPath)
				if sync.BackupPath != "" {
					fmt.Fprintf(os.Stderr, "    backup: %s\n", sync.BackupPath)
				}
			}
			if sync.StatusLineSkipped {
				fmt.Fprintf(os.Stderr, "  → left your own \"statusLine\" in %s untouched\n", settingsPath)
			}
		}

		// Output modes:
		//   --field X    → just that field's value (machine-scrapable)
		//   --json       → the full DeployResult JSON object on stdout
		//   --count-only → just the file count
		//   default      → concise human-readable summary (no JSON wall)
		switch {
		case deployField != "":
			b, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(fieldExtract(string(b), deployField))
		case deployJSON:
			// Human summary on stderr keeps stdout pure JSON for scripts.
			printDeploySummary(result, os.Stderr)
			b, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(b))
		case deployCountOnly:
			fmt.Println(result.FilesDeployed)
		default:
			printDeploySummary(result, os.Stdout)
		}
	},
}

// deploySkillSHACmd prints the deterministic content SHA for a single skill
// directory — the same digest deploy.ComputeSkillSHA writes into the deploy
// manifest. It is the canonical, single-source way to obtain a skill's SHA so
// downstream tooling (notably skills/verify-install/scripts/verify.sh) can shell
// out to the Go implementation instead of re-deriving the algorithm in another
// language. The Python re-implementation in verify.sh is kept only as a fallback
// for hosts where the bravros binary is absent.
//
// The argument is a path to a skill directory (e.g. ~/.claude/skills/plan).
// On success it prints the lower-case hex digest followed by a newline.
var deploySkillSHACmd = &cobra.Command{
	Use:   "skill-sha <skill-dir>",
	Short: "Print the deterministic content SHA for a skill directory",
	Long: `Compute and print the SHA-256 content digest for a single skill directory,
identical to the value deploy writes into ~/.claude/skills/.deploy-manifest.json.

This is the single source of truth for skill-SHA computation. Pass the path to a
skill directory (source or deployed):

  bravros deploy skill-sha ~/.claude/skills/plan

Output: lower-case hex digest on stdout (no trailing noise). Exit 0 on success,
non-zero with a stderr error on a broken symlink or unreadable file.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		skillDir := args[0]
		info, err := os.Stat(skillDir)
		if err != nil {
			return fmt.Errorf("skill-sha: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("skill-sha: %s is not a directory", skillDir)
		}
		sha, err := deploy.ComputeSkillSHA(skillDir)
		if err != nil {
			return fmt.Errorf("skill-sha: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), sha)
		return nil
	},
}

func init() {
	deployCmd.AddCommand(deploySkillSHACmd)
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "List files that would be deployed without copying")
	deployCmd.Flags().BoolVar(&deployCountOnly, "count-only", false, "Only output count of files to deploy")
	deployCmd.Flags().StringVar(&deployField, "field", "", "Extract a single field value (dot notation)")
	deployCmd.Flags().BoolVar(&deployJSON, "json", false, "Emit the full DeployResult JSON object instead of the human summary")
	deployCmd.Flags().BoolVar(&deployForce, "force", false, "Force overwrite every source file at destination, skipping any mtime/hash skip-unchanged comparison; also downgrades the pre-deploy bash-hygiene lint (skill word-split refusal) to a warning")
	deployCmd.Flags().BoolVar(&deployNoPrune, "no-prune", false, "Preserve orphan skills/templates/hooks at the destination instead of removing them (default: prune orphans)")
	deployCmd.Flags().StringVar(&deployFilter, "filter", "", "Comma-separated skill names to deploy; overrides .bravros.yml:skills.enabled (core skills always deploy)")
	deployCmd.Flags().StringVar(&deploySource, "source", "", "Deploy from an explicit source dir instead of cwd — e.g. a payload fetched by 'bravros selfupdate' on a machine with no bravros clone (must contain skills/ and cli/go.mod, or a subset like skills/+templates/ for a fetched payload)")
	deployCmd.MarkFlagsMutuallyExclusive("json", "field", "count-only")
	rootCmd.AddCommand(deployCmd)
}
