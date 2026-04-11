package cmd

import (
	"fmt"
	"os"

	"github.com/bravros/bravros/internal/i18n"
	"github.com/bravros/bravros/internal/updater"
	"github.com/spf13/cobra"
)

var (
	updateForce bool
	updateCheck bool
)

var selfupdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and apply updates from GitHub Releases",
	Long:  "Checks the latest GitHub Release for a newer bravros binary. Downloads and replaces the current binary when an update is available. Uses a 24h cache to avoid excessive network calls.",
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := updater.Check(Version, updateForce)
		if err != nil {
			// Network failure or API error — never block CLI usage.
			return nil
		}
		if result == nil {
			return nil
		}

		// --check mode: print status and exit without modifying anything.
		if updateCheck {
			if result.HasUpdate {
				fmt.Fprintln(os.Stderr, i18n.Tf("update.available", result.Latest, Version))
			} else {
				fmt.Fprintln(os.Stderr, i18n.Tf("update.up_to_date", Version))
			}
			return nil
		}

		// Forced update: current version is below MIN_VERSION.
		if result.IsForced {
			fmt.Fprintln(os.Stderr, i18n.Tf("update.forced", Version, result.Latest))
			if err := updater.Install(result.URL); err != nil {
				fmt.Fprintln(os.Stderr, i18n.Tf("update.failed", err))
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, i18n.Tf("update.success", result.Latest))
			return nil
		}

		// Normal update available: print banner to stderr (non-blocking).
		if result.HasUpdate {
			if updateForce {
				// --force flag: actually perform the update.
				fmt.Fprintln(os.Stderr, i18n.Tf("update.forced", Version, result.Latest))
				if err := updater.Install(result.URL); err != nil {
					fmt.Fprintln(os.Stderr, i18n.Tf("update.failed", err))
					os.Exit(1)
				}
				fmt.Fprintln(os.Stderr, i18n.Tf("update.success", result.Latest))
				return nil
			}
			// Without --force: just show the banner.
			fmt.Fprintln(os.Stderr, i18n.Tf("update.available", result.Latest, Version))
			return nil
		}

		// Up to date — silent.
		return nil
	},
}

func init() {
	selfupdateCmd.Flags().BoolVar(&updateForce, "force", false, "Bypass cache and force update check/install")
	selfupdateCmd.Flags().BoolVar(&updateCheck, "check", false, "Print update status without applying")
}
