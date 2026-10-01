package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Stand-down — machine-wide, time-based
//
// `bravros police standdown on --ttl 4h` gets the suppressible rules out of the
// way for every agent on this machine until the marker expires: the lead
// session, its subagents and workflow workers, and any other agent whose hook
// runs `bravros police pretooluse` (Claude Code, Codex, Antigravity, Grok
// Build). It is deliberately NOT keyed on a session: the previous
// implementation was, and it did nothing in practice — the hook process often
// could not resolve the Claude session env var, the marker sat under a TMPDIR
// that differs between processes, and a stand-down enabled by the lead never
// covered the workers it spawned.
//
// The marker lives at ~/.claude/state/police-standdown.json (next to the
// merge tokens, but it is not one: gateInputRef matches only *-token files).
// The agent may run `standdown on` itself — it relaxes only the rules below
// the safety-floor divider. The floor (rule 52, AI signature, gate-input
// writes) never reads it.
//
// Every command a suppressed rule would have blocked is appended to
// ~/.claude/state/police-standdown-audit.log, so a stand-down leaves a trail.
// ---------------------------------------------------------------------------

const (
	standDownDefaultTTL = 4 * time.Hour
	standDownMaxTTL     = 72 * time.Hour
)

// standDownMarker is the on-disk shape of police-standdown.json.
type standDownMarker struct {
	Scope     string    `json:"scope"` // always "machine"
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	TTL       string    `json:"ttl"`
	Reason    string    `json:"reason,omitempty"`
	SessionID string    `json:"session_id,omitempty"` // informational only: who turned it on
}

func policeStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "state")
}

// standDownPath is the machine-wide marker. "" when HOME cannot be resolved.
func standDownPath() string {
	dir := policeStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "police-standdown.json")
}

func standDownAuditPath() string {
	dir := policeStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "police-standdown-audit.log")
}

// parseStandDownTTL accepts a Go duration (`4h`, `90m`, `1h30m`) or a bare
// integer, read as hours (`6` = 6h).
func parseStandDownTTL(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return standDownDefaultTTL, nil
	}
	var d time.Duration
	bare := false
	if n, err := strconv.Atoi(s); err == nil {
		d, bare = time.Duration(n)*time.Hour, true
	} else if d, err = time.ParseDuration(s); err != nil {
		return 0, fmt.Errorf("invalid --ttl %q: use a duration like 4h or 90m, or a bare number of hours", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid --ttl %q: must be positive", s)
	}
	if d > standDownMaxTTL && bare {
		// A bare 90 reads as 90 minutes to most people; say which unit it was.
		return 0, fmt.Errorf("invalid --ttl %s: --ttl %s means %s hours (bare numbers are hours); max %gh — did you mean %sm?",
			s, s, s, standDownMaxTTL.Hours(), s)
	}
	if d > standDownMaxTTL {
		return 0, fmt.Errorf("invalid --ttl %q: at most %s", s, standDownMaxTTL)
	}
	return d, nil
}

// readStandDown returns the live marker, deleting it when it has expired or
// cannot be parsed. ok is false when no stand-down marker is in force.
func readStandDown() (standDownMarker, bool) {
	path := standDownPath()
	if path == "" {
		return standDownMarker{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return standDownMarker{}, false
	}
	var m standDownMarker
	if json.Unmarshal(data, &m) != nil || m.ExpiresAt.IsZero() || !time.Now().Before(m.ExpiresAt) {
		_ = os.Remove(path) // auto-clean expired or corrupt
		return standDownMarker{}, false
	}
	return m, true
}

// isStandDownActive reports whether the suppressible rules are stood down:
// BRAVROS_POLICE_STANDDOWN=1 in the hook's environment, or a live marker. It
// reads no session id, env var of any agent, or hook payload field.
func isStandDownActive() bool {
	if os.Getenv("BRAVROS_POLICE_STANDDOWN") == "1" {
		return true
	}
	_, ok := readStandDown()
	return ok
}

// auditStandDown appends one line per command a stood-down rule let through.
// Best effort: a logging failure never changes the hook's answer.
func auditStandDown(rule, command string) {
	path := standDownAuditPath()
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	c := strings.ReplaceAll(command, "\n", `\n`)
	if len(c) > 500 {
		c = c[:500] + "…"
	}
	fmt.Fprintf(f, "%s\t%s\t%s\n", time.Now().UTC().Format(time.RFC3339), rule, c)
}

const standDownFloorNote = "Safety floor stays active: irreversible content loss (rule 52), AI attribution, and Bash writes to gate inputs (tokens, review stamps, .bravros/config.json)."

var policeStandDownCmd = &cobra.Command{
	Use:   "standdown",
	Short: "Machine-wide, time-bounded Police stand-down (on/off/status)",
	Long: `Stand the suppressible Police rules down for EVERY agent on this machine —
every session, subagent and workflow worker, whichever agent runs the hook
(Claude Code, Codex, Antigravity, Grok Build) — until the TTL expires.

  bravros police standdown on [--ttl 4h] [--reason "..."]
  bravros police standdown off
  bravros police standdown status

--ttl takes a Go duration (4h, 90m) or a bare number of hours (--ttl 6).
Default 4h, maximum 72h. Run it yourself before a long workflow; no operator
token is needed.

Suppressed while active: the merge gate (pushes/merges into main/master) and the
@claude review-comment template check. Each command they would have blocked is
logged to ~/.claude/state/police-standdown-audit.log.

` + standDownFloorNote + `

Marker: ~/.claude/state/police-standdown.json (scope, created_at, expires_at,
reason). Expired markers are deleted on the next read. The env var
BRAVROS_POLICE_STANDDOWN=1 in the hook's environment has the same effect.`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var (
	standDownTTLFlag    string
	standDownReasonFlag string
)

var policeStandDownOnCmd = &cobra.Command{
	Use:   "on",
	Short: "Stand Police down machine-wide for --ttl (default 4h)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ttl, err := parseStandDownTTL(standDownTTLFlag)
		if err != nil {
			return err
		}
		path := standDownPath()
		if path == "" {
			return fmt.Errorf("police standdown on: cannot resolve the home directory")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		now := time.Now().UTC()
		m := standDownMarker{
			Scope:     "machine",
			CreatedAt: now,
			ExpiresAt: now.Add(ttl),
			TTL:       ttl.String(),
			Reason:    strings.TrimSpace(standDownReasonFlag),
			SessionID: resolveSession(),
		}
		data, _ := json.MarshalIndent(m, "", "  ")
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"✓ Police stand-down ON machine-wide (all sessions and agents) for %s, until %s.\n%s\nSuppressed commands are logged to %s.\nEnd it early: bravros police standdown off\n",
			ttl, m.ExpiresAt.Format(time.RFC3339), standDownFloorNote, standDownAuditPath())
		return nil
	},
}

var policeStandDownOffCmd = &cobra.Command{
	Use:   "off",
	Short: "End the machine-wide Police stand-down",
	RunE: func(cmd *cobra.Command, args []string) error {
		if path := standDownPath(); path != "" {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "✓ Police stand-down marker cleared — every rule is enforced again.")
		return nil
	},
}

var policeStandDownStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report whether Police is stood down, and for how long",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := struct {
			Active      bool   `json:"active"`
			Source      string `json:"source,omitempty"` // env | marker
			Scope       string `json:"scope,omitempty"`
			CreatedAt   string `json:"created_at,omitempty"`
			ExpiresAt   string `json:"expires_at,omitempty"`
			Remaining   string `json:"remaining,omitempty"`
			Reason      string `json:"reason,omitempty"`
			SafetyFloor string `json:"safety_floor"`
		}{SafetyFloor: standDownFloorNote}

		if m, ok := readStandDown(); ok {
			out.Active, out.Source, out.Scope = true, "marker", m.Scope
			out.CreatedAt = m.CreatedAt.Format(time.RFC3339)
			out.ExpiresAt = m.ExpiresAt.Format(time.RFC3339)
			out.Remaining = time.Until(m.ExpiresAt).Round(time.Second).String()
			out.Reason = m.Reason
		} else if os.Getenv("BRAVROS_POLICE_STANDDOWN") == "1" {
			out.Active, out.Source, out.Scope = true, "env", "process"
		}
		emitStandDownStatus(cmd.OutOrStdout(), out)
		return nil
	},
}

func emitStandDownStatus(out io.Writer, v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Fprintln(out, string(data))
}

func init() {
	policeStandDownOnCmd.Flags().StringVar(&standDownTTLFlag, "ttl", "4h", "how long to stand down: a duration (4h, 90m) or a bare number of hours (6)")
	policeStandDownOnCmd.Flags().StringVar(&standDownReasonFlag, "reason", "", "why (recorded in the marker)")
	policeStandDownCmd.AddCommand(policeStandDownOnCmd)
	policeStandDownCmd.AddCommand(policeStandDownOffCmd)
	policeStandDownCmd.AddCommand(policeStandDownStatusCmd)
	policeCmd.AddCommand(policeStandDownCmd)
}
