package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bravros/bravros/cli/internal/hooks"
	"github.com/spf13/cobra"
)

var (
	hooksUpdateForce  bool
	hooksUpdateDryRun bool
	hooksUpdateJSON   bool
)

// hooksUpdateResult is the JSON output shape for `kaisser hooks update --json`.
type hooksUpdateResult struct {
	Refreshed         []string `json:"refreshed"`
	SkippedCustomized []string `json:"skipped_customized"`
	HooksPathSet      bool     `json:"hooks_path_set"`
}

// canonicalHookPath returns the path to the deployed canonical commit-msg hook.
func canonicalHookPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "templates", ".githooks", "commit-msg"), nil
}

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage kaisser-managed git hooks",
}

var hooksUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Refresh commit-msg hook from canonical",
	Long: `Refresh the commit-msg hook in the current repository from the kaisser canonical.

By default:
  - Updates hooks whose content matches a known canonical version (Pristine or OldCanonical).
  - Skips Customized hooks (user-edited, with marker) with a warning to stderr.
  - Skips Missing and Foreign hooks silently.
  - Scans .githooks/commit-msg (always) and .git/hooks/commit-msg (only if .git/hooks/ exists).
  - Calls git config core.hooksPath=.githooks (idempotent) after any refresh.

Use --force to also overwrite Customized hooks.
Use --dry-run to see what would change without writing.
Use --json for machine-parseable output.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		canonical, err := canonicalHookPath()
		if err != nil {
			return err
		}

		// Check canonical exists
		if _, err := os.Stat(canonical); os.IsNotExist(err) {
			return fmt.Errorf("canonical hook not found: %s", canonical)
		}

		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("could not determine working directory: %w", err)
		}

		result := &hooksUpdateResult{
			Refreshed:         []string{},
			SkippedCustomized: []string{},
		}

		// Determine which destinations to scan.
		// Always scan .githooks/commit-msg.
		// Only scan .git/hooks/commit-msg if .git/hooks/ exists.
		destinations := []string{
			filepath.Join(cwd, ".githooks", "commit-msg"),
		}
		gitHooksDir := filepath.Join(cwd, ".git", "hooks")
		if info, err := os.Stat(gitHooksDir); err == nil && info.IsDir() {
			destinations = append(destinations, filepath.Join(gitHooksDir, "commit-msg"))
		}

		hooksPathNeeded := false

		for _, target := range destinations {
			status, err := hooks.Classify(target, canonical)
			if err != nil {
				return fmt.Errorf("failed to classify %s: %w", target, err)
			}

			switch status {
			case hooks.StatusPristine, hooks.StatusOldCanonical:
				// Always refresh these
				if !hooksUpdateDryRun {
					if err := hooks.Refresh(target, canonical); err != nil {
						return fmt.Errorf("failed to refresh %s: %w", target, err)
					}
				}
				result.Refreshed = append(result.Refreshed, target)
				hooksPathNeeded = true

			case hooks.StatusCurrent:
				if hooksUpdateForce {
					// --force: overwrite customized too
					if !hooksUpdateDryRun {
						if err := hooks.Refresh(target, canonical); err != nil {
							return fmt.Errorf("failed to refresh %s: %w", target, err)
						}
					}
					result.Refreshed = append(result.Refreshed, target)
					hooksPathNeeded = true
				} else {
					// Default: skip with warning to stderr
					fmt.Fprintf(os.Stderr, "warning: %s is customized (use --force to overwrite)\n", target)
					result.SkippedCustomized = append(result.SkippedCustomized, target)
				}

			case hooks.StatusMissing, hooks.StatusForeign:
				// Skip silently
			}
		}

		// Call EnsureHooksPath if any file was refreshed (or would be refreshed in dry-run).
		if hooksPathNeeded && !hooksUpdateDryRun {
			if err := hooks.EnsureHooksPath(cwd); err != nil {
				// Non-fatal: warn only
				fmt.Fprintf(os.Stderr, "warning: could not set core.hooksPath: %v\n", err)
			} else {
				result.HooksPathSet = true
			}
		} else if hooksPathNeeded && hooksUpdateDryRun {
			// In dry-run mode, report that hooksPath would be set
			result.HooksPathSet = true
		}

		if hooksUpdateJSON {
			b, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(b))
		} else if len(result.Refreshed) > 0 {
			for _, p := range result.Refreshed {
				verb := "refreshed"
				if hooksUpdateDryRun {
					verb = "would refresh"
				}
				fmt.Fprintf(os.Stdout, "%s: %s\n", verb, p)
			}
		}

		return nil
	},
}

func init() {
	hooksUpdateCmd.Flags().BoolVar(&hooksUpdateForce, "force", false, "Overwrite customized hooks (those with a kaisser marker)")
	hooksUpdateCmd.Flags().BoolVar(&hooksUpdateDryRun, "dry-run", false, "Report what would change without writing")
	hooksUpdateCmd.Flags().BoolVar(&hooksUpdateJSON, "json", false, "Machine-parseable JSON output to stdout")
	hooksCmd.AddCommand(hooksUpdateCmd)
	rootCmd.AddCommand(hooksCmd)
}
