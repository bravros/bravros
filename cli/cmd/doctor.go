package cmd

import (
	"fmt"
	"os"

	"github.com/bravros/bravros/cli/internal/doctor"
	"github.com/spf13/cobra"
)

var (
	doctorQuick          bool
	doctorDeep           bool
	doctorInstallMissing bool
	doctorFix            bool
	doctorJSON           bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run health checks on the bravros installation",
	Long: `Run health checks on the bravros installation.

Modes:
  --quick           Secret-free, headless, safe for SessionStart hook.
                    Skips the 1Password check.
  --deep            Full check: all binaries, 1Password CLI, and extended checks.
  --install-missing Propose safe-listed tool installs.
                    Allowlist: gh, jq, wget, curl, node, pipx, bun, minisign.
                    Never installs: op, claude (require human consent).
  --fix             Repair drift (rewrite bravros-managed blocks in
                    settings.json and CLAUDE.md; user content is preserved).
  --json            Emit structured JSON.  When healthy, stdout is empty
                    (SessionStart-hook contract).

Exit codes:
  0  healthy or degraded (warnings only)
  1  critical failure`,
	RunE: func(cmd *cobra.Command, args []string) error {
		report, err := runDoctor(doctor.RunOpts{
			Quick:          doctorQuick,
			Deep:           doctorDeep,
			InstallMissing: doctorInstallMissing,
			Fix:            doctorFix,
			JSONOutput:     doctorJSON,
		})
		if err != nil {
			return err
		}

		if report.Status == "critical" {
			os.Exit(1)
		}
		return nil
	},
}

// runDoctor executes the doctor engine and prints its report. Split out of
// RunE so tests can exercise the engine wiring without the os.Exit path.
func runDoctor(opts doctor.RunOpts) (*doctor.DoctorReport, error) {
	report, err := doctor.Run(opts)
	if err != nil {
		return report, fmt.Errorf("doctor: %w", err)
	}
	doctor.PrintReport(report, opts.JSONOutput)
	return report, nil
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorQuick, "quick", false, "Quick mode: skip the 1Password check (safe for SessionStart hook)")
	doctorCmd.Flags().BoolVar(&doctorDeep, "deep", false, "Deep mode: all checks including expensive ones")
	doctorCmd.Flags().BoolVar(&doctorInstallMissing, "install-missing", false, "Propose installs for missing safe-listed tools")
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Attempt drift repair (rewrite bravros-managed blocks)")
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "Emit structured JSON; silent on healthy (SessionStart-hook contract)")
	rootCmd.AddCommand(doctorCmd)
}
