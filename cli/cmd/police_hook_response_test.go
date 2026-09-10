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
	for _, tc := range []struct{ name, command, reason string }{
		{"merge", "git push origin main", "Police Block"},
		{"destructive", "git stash drop", "Police Block"},
		{"attribution", `git commit -m "Made with Cursor"`, "AI signature"},
		{"comment", `gh pr comment 90 --body "@claude please review"`, "Police Block"},
		{"allowed", "printf hello", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"tool_name": "Bash", "tool_input": map[string]string{"command": tc.command}, "permission_mode": "bypassPermissions"})
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
