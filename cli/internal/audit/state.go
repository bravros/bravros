package audit

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SessionState manages marker files in /tmp/agent-audit-{sessionID}/.
type SessionState struct {
	Dir string
}

// NewSessionState creates the session directory if needed.
func NewSessionState(sessionID string) *SessionState {
	dir := filepath.Join(os.TempDir(), "agent-audit-"+sessionID)
	os.MkdirAll(dir, 0755)
	return &SessionState{Dir: dir}
}

// Touch creates or updates a marker file.
func (s *SessionState) Touch(name string) {
	os.WriteFile(filepath.Join(s.Dir, name), []byte{}, 0644)
}

// Exists checks if a marker file exists.
func (s *SessionState) Exists(name string) bool {
	_, err := os.Stat(filepath.Join(s.Dir, name))
	return err == nil
}

// ReadText reads a marker file's content as string.
func (s *SessionState) ReadText(name string) string {
	data, err := os.ReadFile(filepath.Join(s.Dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// WriteText writes text to a marker file.
func (s *SessionState) WriteText(name, content string) {
	os.WriteFile(filepath.Join(s.Dir, name), []byte(content), 0644)
}

// AppendLine appends a line to a marker file.
func (s *SessionState) AppendLine(name, line string) {
	f, err := os.OpenFile(filepath.Join(s.Dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(line + "\n")
}

// ReadInt reads a marker file as an integer (returns 0 if not found/invalid).
func (s *SessionState) ReadInt(name string) int {
	text := s.ReadText(name)
	if text == "" {
		return 0
	}
	n, _ := strconv.Atoi(text)
	return n
}

// WriteInt writes an integer to a marker file.
func (s *SessionState) WriteInt(name string, val int) {
	s.WriteText(name, strconv.Itoa(val))
}

// IncrInt increments an integer marker and returns the new value.
func (s *SessionState) IncrInt(name string) int {
	n := s.ReadInt(name) + 1
	s.WriteInt(name, n)
	return n
}
