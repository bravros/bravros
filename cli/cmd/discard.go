package cmd

import (
	"fmt"

	"github.com/bravros/bravros/cli/internal/trash"
	"github.com/spf13/cobra"
)

var discardDryRunFlag bool
var cleanUntrackedDryRunFlag bool

var discardCmd = &cobra.Command{
	Use:   "discard <paths...>",
	Short: "Preserve into .trash/ then discard uncommitted changes (no token)",
	Long: `Sanctioned preserve-then-discard path (audit Rule 52's layer 1).

Copies every file the pathspec covers whose content git has never seen
(uncommitted modifications, untracked files) into .trash/<stamp>-discard/,
THEN discards: tracked modifications via git checkout --, untracked files
by deletion. Reversible via bravros trash restore <id> for 30 days.

  bravros discard app/Foo.php     — one file
  bravros discard .               — everything under the cwd
  bravros discard --dry-run .     — preview without touching the tree`,
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDiscard(cmd, args, false, discardDryRunFlag)
	},
}

var cleanUntrackedCmd = &cobra.Command{
	Use:   "clean-untracked [paths...]",
	Short: "Preserve into .trash/ then remove untracked files (no token)",
	Long: `Sanctioned replacement for git clean (audit Rule 52's layer 1).

Copies every untracked file the pathspec covers into
.trash/<stamp>-clean-untracked/, then removes it. Tracked files are never
touched. No pathspec means the whole repo. Reversible via
bravros trash restore <id> for 30 days.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDiscard(cmd, args, true, cleanUntrackedDryRunFlag)
	},
}

func runDiscard(cmd *cobra.Command, pathspecs []string, untrackedOnly, dryRun bool) error {
	out := cmd.OutOrStdout()
	root, err := trash.RepoRoot("")
	if err != nil {
		return err
	}
	res, err := trash.Discard(root, pathspecs, untrackedOnly, dryRun)
	if err != nil {
		return err
	}
	if res.Empty() {
		fmt.Fprintln(out, "Nothing to discard — no uncommitted content matches the given paths.")
		return nil
	}
	if dryRun {
		fmt.Fprintf(out, "dry-run: would preserve %d file(s) into .trash/ then discard:\n", len(res.Preserved))
		for _, p := range res.CheckedOut {
			fmt.Fprintf(out, "  [checkout] %s\n", p)
		}
		for _, p := range res.Removed {
			fmt.Fprintf(out, "  [remove]   %s\n", p)
		}
		return nil
	}
	if res.EntryID != "" {
		fmt.Fprintf(out, "✓ Preserved %d file(s) into .trash/%s\n", len(res.Preserved), res.EntryID)
	}
	for _, p := range res.CheckedOut {
		fmt.Fprintf(out, "  [discarded] %s\n", p)
	}
	for _, p := range res.Removed {
		fmt.Fprintf(out, "  [removed]   %s\n", p)
	}
	if res.EntryID != "" {
		fmt.Fprintf(out, "Recover within %d days: bravros trash restore %s\n", trash.DefaultRetentionDays, res.EntryID)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(discardCmd)
	rootCmd.AddCommand(cleanUntrackedCmd)
	discardCmd.Flags().BoolVar(&discardDryRunFlag, "dry-run", false, "Preview without touching the tree")
	cleanUntrackedCmd.Flags().BoolVar(&cleanUntrackedDryRunFlag, "dry-run", false, "Preview without touching the tree")
}
