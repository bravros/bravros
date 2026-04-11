package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bravros/private/internal/i18n"
)

// Payload represents the tool call payload from the agent runner.
type Payload struct {
	ToolName  string
	SessionID string
	Input     map[string]interface{}
	FilePath  string
	Command   string
	PatchText string
}

// Logger handles audit logging.
type Logger struct {
	file      *os.File
	timestamp string
	session   string
	tool      string
}

var patchPathRe = regexp.MustCompile(`(?m)^\*\*\* (?:Add File|Update File|Delete File): (.+)$`)

// ParsePayload reads and normalizes the payload from stdin.
func ParsePayload(r io.Reader) (*Payload, error) {
	var raw map[string]interface{}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}

	p := &Payload{}

	// Tool name — multiple key variants
	p.ToolName = normalizeToolName(firstString(raw, "tool_name", "toolName", "tool", "name"))

	// Session ID
	p.SessionID = firstString(raw, "session_id", "sessionId", "run_id", "runId")
	if p.SessionID == "" {
		p.SessionID = "unknown"
	}

	// Tool input
	p.Input = firstMap(raw, "tool_input", "toolInput", "input", "arguments")
	if p.Input == nil {
		p.Input = make(map[string]interface{})
	}

	// Common fields
	p.FilePath = firstInputString(p.Input, "file_path", "filePath", "path")
	p.Command = firstInputString(p.Input, "command", "cmd")
	p.PatchText = firstInputString(p.Input, "patchText", "patch_text")

	return p, nil
}

// PatchPaths extracts file paths from apply_patch-style patch text.
func (p *Payload) PatchPaths() []string {
	if p.PatchText == "" {
		return nil
	}
	matches := patchPathRe.FindAllStringSubmatch(p.PatchText, -1)
	paths := make([]string, 0, len(matches))
	for _, m := range matches {
		paths = append(paths, strings.TrimSpace(m[1]))
	}
	return paths
}

// AllPaths returns all file paths (file_path + patch paths).
func (p *Payload) AllPaths() []string {
	var paths []string
	if p.FilePath != "" {
		paths = append(paths, p.FilePath)
	}
	paths = append(paths, p.PatchPaths()...)
	return paths
}

// IsWriteLike returns true if the tool is a write/edit operation.
func (p *Payload) IsWriteLike() bool {
	return p.ToolName == "write" || p.ToolName == "edit" || p.ToolName == "apply_patch" || p.ToolName == "applypatch"
}

// NewLogger creates a logger for the audit session.
func NewLogger(sessionID, toolName string) *Logger {
	logDir := filepath.Join(os.Getenv("HOME"), ".claude", "hooks", "logs")
	os.MkdirAll(logDir, 0755)

	now := time.Now()
	logFile := filepath.Join(logDir, now.Format("2006-01-02")+".log")
	f, _ := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)

	short := sessionID
	if len(short) > 8 {
		short = short[:8]
	}

	return &Logger{
		file:      f,
		timestamp: now.Format("2006-01-02 15:04:05"),
		session:   short,
		tool:      toolName,
	}
}

// Log writes a message to the log file.
func (l *Logger) Log(msg string) {
	if l.file != nil {
		fmt.Fprintf(l.file, "[%s] [SESSION:%s] [%s] %s\n", l.timestamp, l.session, l.tool, msg)
	}
}

// Warn logs a warning and prints to stderr.
func (l *Logger) Warn(msg string) {
	l.Log("WARN: " + msg)
	fmt.Fprint(os.Stderr, i18n.Tf("audit.prefix_warning", msg)+"\n")
}

// exitFunc is the function called by Block to terminate. Tests can replace it.
var exitFunc = os.Exit

// Block logs a block and prints to stderr, then exits with code 2.
func (l *Logger) Block(msg string) {
	l.Log("BLOCK: " + msg)
	fmt.Fprint(os.Stderr, "\n"+i18n.Tf("audit.prefix_stop", msg)+"\n\n")
	if l.file != nil {
		l.file.Close()
	}
	exitFunc(2)
}

// Close closes the log file.
func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}

// LogCall logs the tool call summary.
func (l *Logger) LogCall(input map[string]interface{}) {
	summaryKeys := []string{"description", "command", "cmd", "file_path", "filePath", "path", "name", "subagent_type", "model", "team_name"}
	var parts []string
	for _, k := range summaryKeys {
		if v, ok := input[k]; ok {
			s := fmt.Sprintf("%v", v)
			if len(s) > 80 {
				s = s[:80]
			}
			parts = append(parts, k+"="+s)
			if len(parts) >= 3 {
				break
			}
		}
	}
	summary := "no-summary"
	if len(parts) > 0 {
		summary = strings.Join(parts, " | ")
	}
	l.Log("CALL | " + summary)
}

// normalizeToolName normalizes tool names across runners.
func normalizeToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	parts := strings.Split(strings.ToLower(name), ".")
	return parts[len(parts)-1]
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch vv := v.(type) {
			case string:
				return vv
			case map[string]interface{}:
				// Nested tool info
				return firstString(vv, "name", "tool_name", "toolName")
			}
		}
	}
	return ""
}

func firstMap(m map[string]interface{}, keys ...string) map[string]interface{} {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if mm, ok := v.(map[string]interface{}); ok {
				return mm
			}
		}
	}
	return nil
}

func firstInputString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}
