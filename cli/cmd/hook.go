// Phase 7 Handoff — Codex hook parity (B-0251)
//
// Codex hooks live in ~/.codex/hooks.json (separate from config.toml).
// The feature flag [features] hooks=true goes in ~/.codex/config.toml.
// The deprecated codex_hooks alias is also written for older Codex builds.
//
// Parity source: ~/.claude/settings.json hooks block (task 7.2)
//
// Resolved hooks.json block stamped by `bravros hook install-codex`:
//
//	{
//	  "__managed_by": "bravros",
//	  "hooks": {
//	    "PreToolUse": [
//	      {
//	        "matcher": ".*",
//	        "hooks": [
//	          { "type": "command", "command": "bravros audit", "__managed_by": "bravros" }
//	        ]
//	      }
//	    ],
//	    "SessionStart": [
//	      {
//	        "hooks": [
//	          { "type": "command", "command": "bravros selfupdate",                 "__managed_by": "bravros" },
//	          { "type": "command", "command": "bravros hook verify-install-check", "__managed_by": "bravros" }
//	        ]
//	      }
//	    ]
//	  }
//	}
//
// Notes:
//   - No macOS $__CFBundleIdentifier guard — Codex CLI has no equivalent mechanism.
//     The selfupdate and verify-install-check commands are safe to run unconditionally.
//   - SessionStart MatcherGroup has no `matcher` field (runs for all start types).
//   - Idempotency: managed hooks are identified by the "__managed_by": "bravros" marker.
//     Re-running install-codex removes stale managed groups and re-inserts the canonical set.
//     User-authored groups (no __managed_by or __managed_by != "bravros") are preserved verbatim.

package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bravros/bravros/cli/internal/secrets"
	"github.com/spf13/cobra"
)

const verifyInstallMarker = ".verify-install-pending"
const secretsSetupNudgedMarker = ".secrets-setup-nudged"
const bravrosStateDirName = "bravros"

// secretsStatusFn is the seam tests use to inject a fake secrets status without
// shelling out to a live op session or reading the test host's ~/.zshenv.
// Production calls secrets.Status().
var secretsStatusFn = secrets.Status

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Claude Code hook subcommands",
	Long:  "Subcommands that are invoked by Claude Code hooks (SessionStart, PreToolUse, etc.).",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var hookVerifyInstallCheckCmd = &cobra.Command{
	Use:   "verify-install-check",
	Short: "Emit SessionStart context if a pending verify-install marker exists",
	Long: `Reads ~/.config/bravros/state/.verify-install-pending, falling back to
~/.claude/state/.verify-install-pending for older Claude installs. If absent or
unreadable, exits silently (exit 0). If present, emits a JSON block to stdout
instructing the host to run /auto-verify-install, then removes the marker so it
fires only once.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil // silent exit
		}

		markerPath := firstExistingVerifyInstallMarker(home)
		if markerPath == "" {
			return nil
		}
		data, err := os.ReadFile(markerPath)
		if err != nil {
			// Missing or unreadable — exit silently
			return nil
		}

		// Parse version from marker content
		version := parseMarkerVersion(string(data))

		// Remove marker before emitting (best-effort; don't block on failure)
		_ = os.Remove(markerPath)

		// Emit SessionStart hook JSON instructing the host to run /auto-verify-install.
		note := fmt.Sprintf(
			"bravros auto-update installed a new version (%s). "+
				"IMPORTANT: immediately invoke the /auto-verify-install skill now "+
				"to confirm the installation is healthy. "+
				"Do not wait for the user to ask — run it as your very first action this session.",
			version,
		)

		// Secrets-setup nudge (piggybacks the one-shot post-upgrade emit). Fires
		// ONLY when: this is a real upgrade (we're here because the verify-install
		// marker existed), op/keychain is the active backend, the SA-token block is
		// genuinely unconfigured ("none"), AND we have never nudged before. The
		// permanent marker makes it once-EVER. Env/none-backend users and
		// already-configured users get nothing.
		st := secretsStatusFn()
		nudgeSecrets := (st.Backend == secrets.BackendOp || st.Backend == secrets.BackendKeychain) &&
			st.SATokenBlock == "none" &&
			!secretsSetupAlreadyNudged(home)
		if nudgeSecrets {
			note += " Additionally, bravros secrets are not yet configured on this machine " +
				"(op/keychain backend, no SA-token block in ~/.zshenv). " +
				"Invoke /secrets-setup to bootstrap it."
			// Stamp the permanent marker so the secrets nudge never fires again,
			// even on the next upgrade.
			writeSecretsSetupNudgedMarker(home)
		}

		type hookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
			SessionStartType  string `json:"sessionStartType"`
		}
		type hookOutput struct {
			HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
		}

		payload := hookOutput{
			HookSpecificOutput: hookSpecificOutput{
				HookEventName:     "SessionStart",
				AdditionalContext: note,
				SessionStartType:  "startup",
			},
		}

		enc, err := json.Marshal(payload)
		if err != nil {
			return nil // silent exit on marshal failure
		}

		fmt.Fprintln(cmd.OutOrStdout(), string(enc))
		return nil
	},
}

func sharedStateDir(home string) string {
	if stateHome := strings.TrimSpace(os.Getenv("BRAVROS_STATE_HOME")); stateHome != "" {
		return filepath.Join(stateHome, "state")
	}
	return filepath.Join(home, ".config", bravrosStateDirName, "state")
}

func legacyClaudeStateDir(home string) string {
	return filepath.Join(home, ".claude", "state")
}

func verifyInstallMarkerPaths(home string) []string {
	return []string{
		filepath.Join(sharedStateDir(home), verifyInstallMarker),
		filepath.Join(legacyClaudeStateDir(home), verifyInstallMarker),
	}
}

func firstExistingVerifyInstallMarker(home string) string {
	for _, markerPath := range verifyInstallMarkerPaths(home) {
		if _, err := os.Stat(markerPath); err == nil {
			return markerPath
		}
	}
	return ""
}

// secretsSetupNudgedMarkerPaths mirrors verifyInstallMarkerPaths for the
// permanent (once-EVER) secrets-setup nudge marker: the shared bravros state dir
// first, then the legacy ~/.claude/state dir.
func secretsSetupNudgedMarkerPaths(home string) []string {
	return []string{
		filepath.Join(sharedStateDir(home), secretsSetupNudgedMarker),
		filepath.Join(legacyClaudeStateDir(home), secretsSetupNudgedMarker),
	}
}

// secretsSetupAlreadyNudged reports whether the permanent secrets-setup nudge
// marker exists in EITHER state dir. If it does, the nudge has fired once before
// and must never fire again.
func secretsSetupAlreadyNudged(home string) bool {
	for _, markerPath := range secretsSetupNudgedMarkerPaths(home) {
		if _, err := os.Stat(markerPath); err == nil {
			return true
		}
	}
	return false
}

// writeSecretsSetupNudgedMarker stamps the permanent secrets-setup nudge marker
// into the shared bravros state dir (best-effort; failures are swallowed because
// the marker is an optimization — a missed write at worst re-nudges on a future
// upgrade, never breaks the session).
func writeSecretsSetupNudgedMarker(home string) {
	markerPath := filepath.Join(sharedStateDir(home), secretsSetupNudgedMarker)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		return
	}
	_ = os.WriteFile(markerPath, []byte("nudged\n"), 0644)
}

// parseMarkerVersion extracts the version value from marker file content.
// Format: "version=<ver>\nts=<iso>\n". Returns "unknown" if not parseable.
func parseMarkerVersion(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "version=") {
			v := strings.TrimPrefix(line, "version=")
			v = strings.TrimSpace(v)
			if v != "" {
				return v
			}
		}
	}
	return "unknown"
}

func init() {
	hookCmd.AddCommand(hookVerifyInstallCheckCmd)
}
