// Package autopr provides helpers for the autonomous pipeline lock subsystem.
// The lock file format is a newline-separated key=value text file stored at
// .planning/.auto-<skill>-lock. This package exposes types and functions that
// are shared between the cmd layer and tests, keeping the cmd package thin.
package autopr

import (
	"os"
	"strings"
	"time"
)

// LockMeta holds the parsed metadata from a lock file.
type LockMeta struct {
	Timestamp time.Time
	Skill     string
	Mode      string
	TTY       string
	SessionID string
	PID       string
}

// ParseLockMeta reads the lock file at path and returns its parsed metadata.
// Returns an error if the file does not exist or cannot be parsed.
func ParseLockMeta(path string) (LockMeta, error) {
	var m LockMeta
	data, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if idx := strings.Index(line, "="); idx > 0 {
			key := line[:idx]
			val := line[idx+1:]
			switch key {
			case "timestamp":
				if ts, parseErr := time.Parse(time.RFC3339, val); parseErr == nil {
					m.Timestamp = ts
				}
			case "skill":
				m.Skill = val
			case "mode":
				m.Mode = val
			case "tty":
				m.TTY = val
			case "session_id":
				m.SessionID = val
			case "pid":
				m.PID = val
			}
		}
	}
	return m, nil
}

// Age returns how long ago the lock was created. Returns 0 if Timestamp is zero.
func (m LockMeta) Age() time.Duration {
	if m.Timestamp.IsZero() {
		return 0
	}
	return time.Since(m.Timestamp)
}

// IsSelfSession returns true when the lock's session_id matches the provided sessionID.
// Both must be non-empty for this to return true.
func (m LockMeta) IsSelfSession(sessionID string) bool {
	return sessionID != "" && m.SessionID != "" && m.SessionID == sessionID
}
