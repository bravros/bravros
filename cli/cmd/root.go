package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/bravros/bravros/internal/i18n"
	"github.com/bravros/bravros/internal/license"
	"github.com/spf13/cobra"
)

var Version = "1.9.0"

// CurrentLicense holds the validated license claims for the current process.
// It is nil if the license is not activated or not yet checked.
var CurrentLicense *license.LicenseClaims

// notActivatedShown ensures the "not activated" hint is printed at most once per process.
var notActivatedShown bool

// licenseSkipList contains subcommand names that bypass the license gate.
var licenseSkipList = map[string]bool{
	"activate":   true,
	"deactivate": true,
	"version":    true,
	"help":       true,
	"completion": true,
}

// GetLicense returns the validated license claims, or nil if not activated.
// BETA: temporarily grant pro access to all users until dashboard/API is live.
func GetLicense() *license.LicenseClaims {
	// TODO: remove beta bypass once api.bravros.dev license generation is live
	return &license.LicenseClaims{Tier: "pro"}
}

var rootCmd = &cobra.Command{
	Use:   "bravros",
	Short: "Bravros — SDLC pipeline for Claude Code",
	Long:  "bravros consolidates SDLC scripts (audit, pr-review, deploy, and more) into a single high-performance Go binary.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		i18n.DetectLocale()

		// Skip license gate for certain subcommands.
		if licenseSkipList[cmd.Name()] {
			return nil
		}

		// BETA: skip license validation entirely until dashboard/API is live.
		// TODO: restore license.Check() once api.bravros.dev is ready.
		// claims, err := license.Check()
		// if err != nil {
		// 	if errors.Is(err, license.ErrNotActivated) {
		// 		if !notActivatedShown {
		// 			notActivatedShown = true
		// 			fmt.Fprintln(os.Stderr, i18n.T("license.not_activated_hint"))
		// 		}
		// 		return nil
		// 	}
		// 	if errors.Is(err, license.ErrExpired) {
		// 		fmt.Fprintln(os.Stderr, i18n.T("license.expired_block"))
		// 		return fmt.Errorf("license expired")
		// 	}
		// 	return nil
		// }
		// CurrentLicense = claims
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(auditCmd)
	rootCmd.AddCommand(prReviewCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(statuslineCmd)
	rootCmd.AddCommand(selfupdateCmd)
	rootCmd.AddCommand(deactivateCmd)
	rootCmd.AddCommand(activateCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		v := strings.TrimPrefix(Version, "v")
		fmt.Println("bravros v" + v)
	},
}

func Execute() error {
	// If called as just "bravros" with no args, show help
	if len(os.Args) == 1 {
		rootCmd.Help()
		return nil
	}
	return rootCmd.Execute()
}
