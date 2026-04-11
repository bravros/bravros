package cmd

import (
	"errors"
	"fmt"

	"github.com/bravros/bravros/internal/i18n"
	"github.com/bravros/bravros/internal/license"
	"github.com/spf13/cobra"
)

var activateCmd = &cobra.Command{
	Use:   "activate LICENSE-KEY",
	Short: "Activate Bravros with a license key",
	Long:  "Activate Bravros on this machine using your license key (XXXX-XXXX-XXXX-XXXX). The key is validated, sent to the Bravros API, and the resulting token is cached locally.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]

		// Validate format before hitting the network.
		if !license.ValidateLicenseKey(key) {
			fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("license.err_invalid_key"))
			return fmt.Errorf("invalid license key format")
		}

		machineID := license.MachineID()

		token, err := license.DefaultClient.Activate(key, machineID)
		if err != nil {
			var apiErr *license.APIError
			if errors.As(err, &apiErr) {
				switch apiErr.Code {
				case "already_active":
					fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("license.err_already_active"))
				case "machine_limit":
					fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("license.err_machine_limit"))
				case "invalid_key":
					fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("license.err_invalid_key"))
				default:
					fmt.Fprintln(cmd.ErrOrStderr(), apiErr.Error())
				}
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("license.err_network"))
			}
			return err
		}

		if err := license.SaveToken(token); err != nil {
			return fmt.Errorf("activate: failed to save token: %w", err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), i18n.T("license.activate_success"))
		return nil
	},
}
