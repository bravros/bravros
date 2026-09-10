package cmd

import (
	"encoding/json"
	"io"
)

// writePoliceDeny uses the host's PreToolUse decision contract. A JSON field
// named exitCode is NOT a process exit status and is not interpreted as a deny.
// With process success, the host reads hookSpecificOutput.permissionDecision;
// this denial also applies in bypassPermissions mode.
// https://code.claude.com/docs/en/hooks#pretooluse-decision-control
func writePoliceDeny(w io.Writer, reason string) error {
	return json.NewEncoder(w).Encode(struct {
		Output struct {
			Event    string `json:"hookEventName"`
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}{Output: struct {
		Event    string `json:"hookEventName"`
		Decision string `json:"permissionDecision"`
		Reason   string `json:"permissionDecisionReason"`
	}{"PreToolUse", "deny", reason}})
}
