package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Assert the public protocol independently of the production response type.
func assertPoliceDeny(t *testing.T, data []byte) string {
	t.Helper()
	var response map[string]json.RawMessage
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("invalid host JSON: %v: %s", err, data)
	}
	if len(response) != 1 {
		t.Fatalf("unexpected host response keys: %s", data)
	}
	var decision map[string]string
	if err := json.Unmarshal(response["hookSpecificOutput"], &decision); err != nil {
		t.Fatalf("missing host decision: %v: %s", err, data)
	}
	if decision["hookEventName"] != "PreToolUse" || decision["permissionDecision"] != "deny" || decision["permissionDecisionReason"] == "" {
		t.Fatalf("invalid PreToolUse denial: %s", data)
	}
	return decision["permissionDecisionReason"]
}

// Build the real entry point: RunE-only tests cannot prove that the subprocess
// exits successfully or that Cobra does not add non-JSON text to its output.
func TestPoliceHookExecutableContract(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "bravros")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	build := exec.Command("go", "build", "-o", executable, ".")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	home := t.TempDir()
	cwd := t.TempDir()
	if out, err := exec.Command("git", "init", cwd).CombinedOutput(); err != nil {
		t.Fatalf("init scratch repo: %v: %s", err, out)
	}
	// A `gh` PATH shim answers the merge gate's PR lookup with a CLEAN
	// homolog→main PR, so the staging-lane cases below never hit the forge. It
	// is installed before the env slice is built, so the subprocess inherits it.
	fakePRView(t, "main", "homolog", "CLEAN", "", laneOID, false)
	lock := filepath.Join(cwd, ".planning", ".auto-pr-lock")
	for _, tc := range []struct {
		name, command, reason string
		setup                 func()
	}{
		{"merge", "git push origin main", "Police Block", nil},
		{"destructive", "git stash drop", "Police Block", nil},
		{"attribution", `git commit -m "Made with Cursor"`, "AI signature", nil},
		{"comment", `gh pr comment 90 --body "@claude please review"`, "Police Block", nil},
		{"allowed", "printf hello", "", nil},
		// Staging lane: a CLEAN homolog→main merge is allowed silently…
		{"staging-lane", "gh pr merge 7 --merge", "", nil},
		// …and the same shape with a `cd <cwd>` prefix once the payload says so.
		{"staging-lane-cd-cwd", "cd " + cwd + " && gh pr merge 7 --merge", "", nil},
		// Adversarial-review shapes, against the real binary and a hand-made
		// payload, exactly as the reviewer reproduced them.
		{"gh-redefined", `gh(){ command gh "$@" -R o/r; }; gh pr merge 7 --merge`, "redefines gh/git", nil},
		{"gh-alias", `alias gh='gh -R o/r'; shopt -s expand_aliases; gh pr merge 7`, "redefines gh/git", nil},
		{"gh-path-form", "/usr/local/bin/gh pr merge 7 -R o/r --merge", "never use the lane", nil},
		{"gh-substituted", `"$(command -v gh)" pr merge 7 -R o/r --merge`, "named by a substitution", nil},
		{"git-path-form", "/usr/bin/git push origin main", "Merging or pushing to main", nil},
		{"lane-delete-branch", "gh pr merge 7 --merge --delete-branch", "--delete-branch/-d", nil},
		{"lane-var-pr", `gh pr merge "$PR" --merge`, "put the literal PR number", nil},
		{"token-forge", "touch " + home + "/.claude/state/police-token", "gate's own input", nil},
		{"stamp-forge", `printf '{"commit_sha":"abc"}' > .planning/.review-stamp-7.json`, "gate's own input", nil},
		{"config-widen", `printf '{"staging_branch":"feature/x"}' > .bravros/config.json`, "gate's own input", nil},
		{"token-read", "cat " + home + "/.claude/state/police-token; bravros police status", "", nil},
		// …until an autonomous lock closes it.
		{"lane-lock", "gh pr merge 7 --merge", "autonomous lock", func() {
			if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(lock, []byte("skill=auto-pr\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup()
			}
			payload, err := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]string{"command": tc.command}, "permission_mode": "bypassPermissions", "cwd": cwd})
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(executable, "police", "pretooluse")
			cmd.Dir = cwd
			for _, entry := range os.Environ() {
				key := strings.SplitN(entry, "=", 2)[0]
				switch key {
				case "HOME", "USERPROFILE", "BRAVROS_POLICE_STANDDOWN", "CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID":
					continue
				}
				cmd.Env = append(cmd.Env, entry)
			}
			cmd.Env = append(cmd.Env, "HOME="+home, "USERPROFILE="+home, "BRAVROS_POLICE_STANDDOWN=", "CLAUDE_SESSION_ID=", "CLAUDE_CODE_SESSION_ID=")
			cmd.Stdin = bytes.NewReader(payload)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("hook process must exit 0 for JSON decision: %v; stderr=%s", err, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr: %s", stderr.String())
			}
			if tc.reason == "" {
				if stdout.Len() != 0 {
					t.Fatalf("allowed command must be silent: %s", stdout.String())
				}
			} else if reason := assertPoliceDeny(t, stdout.Bytes()); !strings.Contains(reason, tc.reason) {
				t.Fatalf("reason %q missing %q", reason, tc.reason)
			}
		})
	}
	// The two lane merges above were allowed without a token, so both must be
	// on the audit log under the subprocess's HOME.
	audit, err := os.ReadFile(filepath.Join(home, ".claude", "state", "police-merge-audit.log"))
	if err != nil {
		t.Fatalf("lane merges must be audited: %v", err)
	}
	if n := strings.Count(string(audit), "pr=7 homolog->main mode=open"); n != 2 {
		t.Fatalf("want 2 audit lines, got %d:\n%s", n, audit)
	}
}

func TestPoliceDestructiveTokenDoesNotAuthorizeOtherGates(t *testing.T) {
	for _, tc := range []struct{ name, command, reason string }{
		{"standalone", "git stash drop", ""},
		{"merge", "git stash drop; git push origin main", "Merging or pushing to main"},
		{"attribution", `git stash drop; git commit -m "Made with Cursor"`, "AI signature"},
		{"comment", `git stash drop; gh pr comment 90 --body "@claude please review"`, "Police Block"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := rule52TestSetEnv(t)
			rule52TestChdir(t, rule52TestNewRepo(t))
			tokenPath := rule52TestWriteDestructiveToken(t, home, time.Now().Add(time.Minute))
			blocked, _, reason := rule52TestInvoke(t, tc.command)
			if blocked != (tc.reason != "") || !strings.Contains(reason, tc.reason) {
				t.Fatalf("blocked=%v reason=%q; expected %q", blocked, reason, tc.reason)
			}
			if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
				t.Fatalf("destructive token was not consumed: %v", err)
			}
			if blocked, _, _ := rule52TestInvoke(t, "git stash drop"); !blocked {
				t.Fatal("destructive token was reusable")
			}
		})
	}
}
