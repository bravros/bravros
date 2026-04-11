package cmd

import (
	"errors"
	"fmt"

	"github.com/bravros/bravros/internal/i18n"
	"github.com/bravros/bravros/internal/license"
	"github.com/spf13/cobra"
)

var deactivateCmd = &cobra.Command{
	Use:   "deactivate",
	Short: "Deactivate Bravros license on this machine",
	Long:  "Deactivates the Bravros license for this machine, freeing the license slot.",
	RunE: func(cmd *cobra.Command, args []string) error {
		token, err := license.LoadToken()
		if err != nil {
			if errors.Is(err, license.ErrNotActivated) {
				fmt.Fprintln(cmd.OutOrStdout(), i18n.T("license.err_not_activated"))
				return nil
			}
			return err
		}

		machineID := license.MachineID()
		apiErr := license.DefaultClient.Deactivate(token, machineID)

		// Always clear local cache, even on network failure
		if clearErr := license.ClearToken(); clearErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to clear local cache: %v\n", clearErr)
		}

		if apiErr != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("license.err_network"))
			fmt.Fprintln(cmd.OutOrStdout(), i18n.T("license.local_cache_cleared"))
			return nil
		}

		fmt.Fprintln(cmd.OutOrStdout(), i18n.T("license.deactivate_success"))
		return nil
	},
}
