package cmd

import (
	"fmt"

	"github.com/bravros/bravros/cli/internal/trash"
	"github.com/spf13/cobra"
)

var trashGCDaysFlag int

var trashCmd = &cobra.Command{
	Use:   "trash",
	Short: "Inspect and manage the .trash/ preserve area",
	Long: `The .trash/ preserve area holds copies made by bravros discard and
bravros clean-untracked before they discard uncommitted content.

bravros trash list           — entries with age, file count and size
bravros trash restore <id>   — copy an entry's files back (byte-identical)
bravros trash gc [--days N]  — reap entries older than N days (default 30)

The area lives at <repo-root>/.trash/ (gitignored) and is also swept by
bravros branch prune --gc.`,
}

var trashListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List .trash/ entries",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		root, err := trash.RepoRoot("")
		if err != nil {
			return err
		}
		entries, err := trash.List(root)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Fprintln(out, "No trash entries.")
			return nil
		}
		for _, e := range entries {
			fmt.Fprintf(out, "%s  %d file(s)  %d bytes  %s\n",
				e.ID, e.Files, e.Bytes, e.CreatedAt.Format("2006-01-02 15:04 MST"))
		}
		return nil
	},
}

var trashRestoreCmd = &cobra.Command{
	Use:          "restore <id>",
	Short:        "Restore a .trash/ entry's files to their original locations",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := trash.RepoRoot("")
		if err != nil {
			return err
		}
		n, err := trash.Restore(root, args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Restored %d file(s) from .trash/%s\n", n, args[0])
		return nil
	},
}

var trashGCCmd = &cobra.Command{
	Use:          "gc",
	Short:        "Reap .trash/ entries older than the retention window",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		root, err := trash.RepoRoot("")
		if err != nil {
			return err
		}
		removed, err := trash.GC(root, trashGCDaysFlag)
		if err != nil {
			return err
		}
		if len(removed) == 0 {
			fmt.Fprintln(out, "No trash entries eligible for GC.")
			return nil
		}
		fmt.Fprintf(out, "GC'd %d trash entr%s:\n", len(removed), pluralY(len(removed)))
		for _, id := range removed {
			fmt.Fprintf(out, "  %s\n", id)
		}
		return nil
	},
}

// pluralY returns "y"/"ies" for count-based entry pluralization.
func pluralY(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func init() {
	rootCmd.AddCommand(trashCmd)
	trashCmd.AddCommand(trashListCmd)
	trashCmd.AddCommand(trashRestoreCmd)
	trashCmd.AddCommand(trashGCCmd)
	trashGCCmd.Flags().IntVar(&trashGCDaysFlag, "days", trash.DefaultRetentionDays, "Retention window in days")
}
