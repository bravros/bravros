// Package token provides a reusable out-of-band human-presence token gate.
//
// A Gate writes a small JSON token to ~/.claude/state/<Name>-token. The token
// proves a human minted it from a terminal OUTSIDE Claude Code (the mint helper
// refuses to run inside an agent session). Consumers — the /promote and
// verify-suite flows plus audit Rules 17b/47 — read the token by path to gate
// privileged actions.
//
// The on-disk path and JSON shape are a stable contract: audit rules read these
// files directly without importing this package, so the field tags and path
// layout MUST stay byte-compatible with the historical promote/verify-suite
// tokens (~/.claude/state/promote-token, ~/.claude/state/verify-suite-token).
package token

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Token is the JSON shape written to ~/.claude/state/<Name>-token.
//
// Field tags are load-bearing: audit Rule 17b/47 unmarshal these files with
// their own structs keyed on the same tags. Do not rename a json tag without
// updating every independent reader.
type Token struct {
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	TTLMinutes int       `json:"ttl_minutes"`
	TTY        string    `json:"tty"`
	SingleUse  bool      `json:"single_use"`
	// Reason is the operator-supplied justification recorded at mint time
	// (destructive gate's `--reason`). omitempty keeps historical tokens and
	// gates that never set it byte-compatible.
	Reason string `json:"reason,omitempty"`
}

// Expired reports whether the token's expiry has passed.
func (t *Token) Expired() bool {
	return time.Now().After(t.ExpiresAt)
}

// Gate is an out-of-band token gate identified by Name. The token file lives at
// ~/.claude/state/<Name>-token. The message fields tailor the CLI output for the
// owning command (e.g. "promote" vs "verify-suite").
type Gate struct {
	// Name is the token identifier; the file is ~/.claude/state/<Name>-token.
	Name string
	// UnlockHelp is the multi-line guidance printed when minting is refused
	// because the command is running inside Claude Code. It typically lists the
	// "open a separate terminal / run unlock / return" steps and a suggested
	// alias. Printed verbatim to stderr.
	UnlockHelp string
	// SuccessMsg is the single-line confirmation printed after a successful mint.
	// It should NOT include the TTL/path/expiry lines — Mint callers print those.
	SuccessMsg string
	// RefuseMsg is the headline printed to stderr when minting is refused inside
	// Claude Code (e.g. "✋ kaisser promote unlock REFUSED: running inside Claude Code.").
	RefuseMsg string
}

// Path returns the canonical on-disk path for this gate's token, or "" when the
// home directory cannot be determined. The token lives in user-scope state
// (~/.claude/state/<Name>-token) so kaisser can read it from any project
// without per-repo bookkeeping.
func (g Gate) Path() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "state", g.Name+"-token")
}

// Read reads and parses the token from disk. Returns nil when the token is
// absent, unreadable, or malformed.
func (g Gate) Read() *Token {
	path := g.Path()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil
	}
	return &tok
}

// Present returns true when a non-expired token exists. Expired tokens are
// auto-cleaned (deleted) as a side effect, matching the historical
// promoteTokenPresent/verifySuiteTokenPresent behavior.
func (g Gate) Present() bool {
	tok := g.Read()
	if tok == nil {
		return false
	}
	if tok.Expired() {
		os.Remove(g.Path()) //nolint:errcheck
		return false
	}
	return true
}

// Valid returns true when a present, unexpired, well-formed token exists. Unlike
// Present it does NOT mutate disk — it never deletes an expired token. Use it for
// pure validity checks; use Present when expired-token auto-cleanup is desired.
func (g Gate) Valid() bool {
	tok := g.Read()
	if tok == nil {
		return false
	}
	return !tok.Expired()
}

// Mint writes a single-use, time-limited token to disk with the given TTL in
// minutes (values <= 0 default to 5). tty records the originating terminal (may
// be ""). It returns the minted token so callers can print the TTL/path/expiry
// follow-up lines, or an error if the state dir or file could not be written.
//
// Mint does NOT perform the inside-Claude refusal check — callers gate that
// before minting (the refusal needs the gate's RefuseMsg/UnlockHelp and an
// os.Exit, which is a command-layer concern).
func (g Gate) Mint(ttl int, tty string) (*Token, error) {
	return g.MintWithReason(ttl, tty, "")
}

// MintWithReason is Mint with an operator-supplied justification recorded in
// the token (consumers may surface it in their audit logs).
func (g Gate) MintWithReason(ttl int, tty, reason string) (*Token, error) {
	if ttl <= 0 {
		ttl = 5
	}
	path := g.Path()
	stateDir := filepath.Dir(path)
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tok := Token{
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Duration(ttl) * time.Minute),
		TTLMinutes: ttl,
		TTY:        tty,
		SingleUse:  true,
		Reason:     reason,
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	return &tok, nil
}

// Revoke deletes the token file. It returns the error from os.Remove unchanged
// (including an os.IsNotExist error) so callers can distinguish "nothing to
// revoke" from a real failure.
func (g Gate) Revoke() error {
	path := g.Path()
	if path == "" {
		return os.ErrInvalid
	}
	return os.Remove(path)
}
