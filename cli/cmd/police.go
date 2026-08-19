package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var policeCmd = &cobra.Command{
	Use:   "police",
	Short: "Bravros police engine (audit and enforcement)",
}

// policePreToolUseCmd intercepts tool usage.
var policePreToolUseCmd = &cobra.Command{
	Use:   "pretooluse",
	Short: "Hook for PreToolUse intercept",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Read stdin for the payload
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return nil
		}
		var payload struct {
			ToolName string `json:"toolName"`
			Input    struct {
				Command string `json:"command"`
			} `json:"input"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil
		}

		// Only intercept bash commands
		if payload.ToolName != "bash" && payload.ToolName != "Bash" {
			return nil
		}

		command := payload.Input.Command
		if isMainMerge(command) {
			if isStandDownActive() {
				return nil
			}
			if !hasValidPoliceToken() {
				// Block execution
				out := struct {
					Stdout string `json:"stdout"`
					Stderr string `json:"stderr"`
					Exit   int    `json:"exitCode"`
				}{
					Stdout: "",
					Stderr: "✋🏽 Police Block: Merging or pushing to main is blocked for agents.\n" +
						"You must request the operator to mint a token from outside Claude Code:\n" +
						"  bravros police unlock\n",
					Exit: 2,
				}
				enc, _ := json.Marshal(out)
				fmt.Fprintln(cmd.OutOrStdout(), string(enc))
				return nil
			}
		}
		return nil
	},
}

// protectedBranches are the only branches the Police gate defends. Everything
// else — homolog included — is routinely pushed and merged by /push, /hotfix,
// /finish, /batch-merge-prs and /auto-pr, and must never need a token.
var protectedBranches = map[string]bool{"main": true, "master": true}

// isMainMerge reports whether cmd would push to, or merge a PR into, a
// protected branch.
//
// Matching is word-boundary based, not substring: `git push origin
// fix/maintain-cache` contains "main" but targets nothing protected. A bare
// `git push` is resolved against the current branch, and a `gh pr merge` is
// resolved against the PR's real base — neither is knowable from the string
// alone.
func isMainMerge(cmd string) bool {
	for _, seg := range commandSegments(cmd) {
		switch {
		case startsWith(seg, "git", "push"):
			if pushTargetsProtected(seg) {
				return true
			}
		case startsWith(seg, "gh", "pr", "merge"):
			if mergeTargetsProtected(seg) {
				return true
			}
		}
	}
	return false
}

// unquote strips matching leading and trailing single or double quotes from s.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// commandSegments splits a command line on unquoted shell separators (&&, ||, ;, |, \n),
// respecting single quotes, double quotes, backslash escapes, and subshell parentheses.
func commandSegments(cmd string) [][]string {
	var segs [][]string
	var cur []string
	var token strings.Builder
	var inSingle, inDouble, escaped bool

	flushToken := func() {
		if token.Len() > 0 {
			t := token.String()
			token.Reset()
			t = strings.Trim(t, "()")
			if t != "" {
				cur = append(cur, unquote(t))
			}
		}
	}

	flushSegment := func() {
		flushToken()
		if len(cur) > 0 {
			segs = append(segs, cur)
			cur = []string{}
		}
	}

	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			token.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			token.WriteRune(r)
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			token.WriteRune(r)
			continue
		}

		if inSingle || inDouble {
			token.WriteRune(r)
			continue
		}

		// Unquoted separators
		if r == ';' || r == '\n' {
			flushSegment()
			continue
		}
		if r == '&' && i+1 < len(runes) && runes[i+1] == '&' {
			flushSegment()
			i++
			continue
		}
		if r == '|' && i+1 < len(runes) && runes[i+1] == '|' {
			flushSegment()
			i++
			continue
		}
		if r == '|' {
			flushSegment()
			continue
		}
		if r == '(' || r == ')' {
			flushToken()
			continue
		}

		if r == ' ' || r == '\t' {
			flushToken()
			continue
		}

		token.WriteRune(r)
	}
	flushSegment()
	return segs
}

// startsWith reports whether the segment's leading words are exactly words,
// ignoring any `env VAR=x` style prefix assignments.
func startsWith(seg []string, words ...string) bool {
	for len(seg) > 0 && strings.Contains(seg[0], "=") {
		seg = seg[1:]
	}
	if len(seg) < len(words) {
		return false
	}
	for i, w := range words {
		if unquote(seg[i]) != w {
			return false
		}
	}
	return true
}

// pushTargetsProtected inspects the refspecs of a `git push`. Each refspec is
// split on ':' so `HEAD:main` and `+main:main` are caught, and leading '+' is
// stripped. With no refspec at all the push follows the tracked upstream, so
// the current branch decides.
func pushTargetsProtected(fields []string) bool {
	refspecs := 0
	for _, f := range fields {
		f = unquote(f)
		if strings.HasPrefix(f, "-") || f == "git" || f == "push" {
			continue
		}
		if f == "origin" || strings.Contains(f, "@") || strings.Contains(f, "://") {
			continue
		}
		refspecs++
		for _, part := range strings.Split(strings.TrimPrefix(f, "+"), ":") {
			part = unquote(strings.TrimPrefix(part, "refs/heads/"))
			if protectedBranches[part] {
				return true
			}
		}
	}
	if refspecs == 0 {
		return protectedBranches[currentBranch()]
	}
	return false
}

// mergeTargetsProtected resolves the base branch of the PR named in a
// `gh pr merge`. The base is not in the command string, so it is read from the
// forge. If that lookup cannot answer — offline, no auth, no PR — the command
// is allowed through: the merge-to-main path is independently gated by
// /promote's out-of-band token and the review stamp, and a hook that blocked
// whenever the network hiccuped would strand every routine homolog merge.
func mergeTargetsProtected(fields []string) bool {
	args := []string{"pr", "view", "--json", "baseRefName", "-q", ".baseRefName"}
	for _, f := range fields {
		if _, err := strconv.Atoi(f); err == nil {
			args = append(args, f)
			break
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "gh", args...).Output()
	if err != nil {
		return false
	}
	return protectedBranches[strings.TrimSpace(string(out))]
}

var policeUnlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Mint a human-presence token to allow merging",
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getenv("CLAUDE_CODE_SESSION_ID") != "" || os.Getenv("CLAUDE_SESSION_ID") != "" {
			return fmt.Errorf("bravros police unlock MUST be run from a separate terminal, outside of Claude Code")
		}

		path := tokenPath()
		err := os.MkdirAll(filepath.Dir(path), 0755)
		if err != nil {
			return err
		}
		// Write token
		err = os.WriteFile(path, []byte("valid\n"), 0644)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "🔓 Police token minted. Agents can now merge to main.")
		return nil
	},
}

var policeRevokeCmd = &cobra.Command{
	Use:   "revoke",
	Short: "Revoke the human-presence token",
	RunE: func(cmd *cobra.Command, args []string) error {
		path := tokenPath()
		_ = os.Remove(path)
		fmt.Fprintln(cmd.OutOrStdout(), "🔒 Police token revoked.")
		return nil
	},
}

var policeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check police token status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if hasValidPoliceToken() {
			fmt.Fprintln(cmd.OutOrStdout(), "🔓 Police token is VALID.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "🔒 Police token is MISSING or INVALID.")
		}
		return nil
	},
}

func tokenPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "state", "police-token")
}

func hasValidPoliceToken() bool {
	info, err := os.Stat(tokenPath())
	if err != nil {
		return false
	}
	// e.g. 10 minutes expiry
	if time.Since(info.ModTime()) > 10*time.Minute {
		os.Remove(tokenPath())
		return false
	}
	return true
}

func init() {
	policeCmd.AddCommand(policePreToolUseCmd)
	policeCmd.AddCommand(policeUnlockCmd)
	policeCmd.AddCommand(policeRevokeCmd)
	policeCmd.AddCommand(policeStatusCmd)
	rootCmd.AddCommand(policeCmd)
}

var policeStandDownCmd = &cobra.Command{
	Use:   "standdown",
	Short: "Manage Police stand-down state",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var standDownTTLFlag time.Duration

var policeStandDownOnCmd = &cobra.Command{
	Use:   "on",
	Short: "Enable Police stand-down for this session",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := resolveSession()
		if sessionID == "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "police standdown on: no agent session detected. (Use env BRAVROS_POLICE_STANDDOWN=1 outside Claude)")
			return nil
		}
		ttl := standDownTTLFlag
		if ttl <= 0 {
			ttl = 4 * time.Hour
		}

		path := standDownPath(sessionID)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}

		now := time.Now().UTC()
		type marker struct {
			SessionID string    `json:"session_id"`
			ExpiresAt time.Time `json:"expires_at"`
			CreatedAt time.Time `json:"created_at"`
			TTL       string    `json:"ttl"`
		}
		m := marker{
			SessionID: sessionID,
			ExpiresAt: now.Add(ttl),
			CreatedAt: now,
			TTL:       ttl.String(),
		}
		data, _ := json.MarshalIndent(m, "", "  ")

		// write via temp file
		tmpPath := path + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err != nil {
			return err
		}
		if err := os.Rename(tmpPath, path); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "✓ Police stand-down ON for this session (TTL %s). Only safety floor active until %s.\n", ttl, m.ExpiresAt.Format(time.RFC3339))
		return nil
	},
}

var policeStandDownOffCmd = &cobra.Command{
	Use:   "off",
	Short: "Disable Police stand-down for this session",
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := resolveSession()
		if sessionID == "" {
			return nil
		}
		path := standDownPath(sessionID)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "✓ Police stand-down marker cleared.")
		return nil
	},
}

var policeStandDownStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report Police stand-down status",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := struct {
			Active    bool   `json:"active"`
			Source    string `json:"source"`
			SessionID string `json:"session_id"`
			ExpiresAt string `json:"expires_at,omitempty"`
		}{
			SessionID: resolveSession(),
		}

		if os.Getenv("BRAVROS_POLICE_STANDDOWN") == "1" {
			out.Active = true
			out.Source = "env"
			emitStandDownStatus(cmd.OutOrStdout(), out)
			return nil
		}

		sessionID := resolveSession()
		if sessionID != "" {
			path := standDownPath(sessionID)
			data, err := os.ReadFile(path)
			if err == nil {
				var m struct {
					SessionID string    `json:"session_id"`
					ExpiresAt time.Time `json:"expires_at"`
				}
				if json.Unmarshal(data, &m) == nil && m.SessionID == sessionID {
					if !m.ExpiresAt.IsZero() && time.Now().Before(m.ExpiresAt) {
						out.Active = true
						out.Source = "marker"
						out.ExpiresAt = m.ExpiresAt.Format(time.RFC3339)
					} else {
						os.Remove(path) // auto-clean expired
					}
				}
			}
		}

		emitStandDownStatus(cmd.OutOrStdout(), out)
		return nil
	},
}

func emitStandDownStatus(out io.Writer, v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Fprintln(out, string(data))
}

func standDownPath(sessionID string) string {
	tmpDir := os.TempDir()
	return filepath.Join(tmpDir, "agent-audit-"+sessionID, "standdown.json")
}

func isStandDownActive() bool {
	if os.Getenv("BRAVROS_POLICE_STANDDOWN") == "1" {
		return true
	}
	sessionID := resolveSession()
	if sessionID == "" {
		return false
	}
	data, err := os.ReadFile(standDownPath(sessionID))
	if err != nil {
		return false
	}
	var m struct {
		SessionID string    `json:"session_id"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if json.Unmarshal(data, &m) == nil && m.SessionID == sessionID {
		if !m.ExpiresAt.IsZero() && time.Now().Before(m.ExpiresAt) {
			return true
		}
	}
	return false
}

func init() {
	policeStandDownOnCmd.Flags().DurationVar(&standDownTTLFlag, "ttl", 4*time.Hour, "Stand-down marker TTL")
	policeStandDownCmd.AddCommand(policeStandDownOnCmd)
	policeStandDownCmd.AddCommand(policeStandDownOffCmd)
	policeStandDownCmd.AddCommand(policeStandDownStatusCmd)
	policeCmd.AddCommand(policeStandDownCmd)
}
