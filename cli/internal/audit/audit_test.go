package audit

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bravros/private/internal/i18n"
)

func TestMain(m *testing.M) {
	// Ensure all audit tests run with English locale so translated message
	// assertions match the expected English text.
	i18n.SetLocale("en")
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTempState creates a SessionState in a temp directory for testing.
func newTempState(t *testing.T) *SessionState {
	t.Helper()
	return &SessionState{Dir: t.TempDir()}
}

// newTestLogger creates a logger that writes nowhere (quiet for tests).
func newTestLogger() *Logger {
	return &Logger{
		timestamp: "2026-01-01 00:00:00",
		session:   "test",
		tool:      "bash",
	}
}

// didBlock runs fn and returns true if Logger.Block was called (i.e. exitFunc
// was invoked). We replace exitFunc with a function that panics with a sentinel,
// then recover it.
func didBlock(fn func()) (blocked bool) {
	orig := exitFunc
	exitFunc = func(code int) { panic("audit-block") }
	defer func() {
		exitFunc = orig
		if r := recover(); r != nil {
			if r == "audit-block" {
				blocked = true
			} else {
				panic(r)
			}
		}
	}()
	fn()
	return false
}

// ---------------------------------------------------------------------------
// ParsePayload
// ---------------------------------------------------------------------------

func TestParsePayload_BasicFields(t *testing.T) {
	tests := []struct {
		name     string
		raw      map[string]interface{}
		wantTool string
		wantSess string
		wantCmd  string
		wantFile string
	}{
		{
			name: "standard bash command",
			raw: map[string]interface{}{
				"tool_name":  "Bash",
				"session_id": "abc123",
				"tool_input": map[string]interface{}{"command": "ls -la"},
			},
			wantTool: "bash",
			wantSess: "abc123",
			wantCmd:  "ls -la",
		},
		{
			name: "camelCase keys",
			raw: map[string]interface{}{
				"toolName":  "Write",
				"sessionId": "sess-42",
				"toolInput": map[string]interface{}{"file_path": "/tmp/foo.txt"},
			},
			wantTool: "write",
			wantSess: "sess-42",
			wantFile: "/tmp/foo.txt",
		},
		{
			name: "namespaced tool name normalized",
			raw: map[string]interface{}{
				"tool_name":  "computer.Bash",
				"session_id": "x",
				"tool_input": map[string]interface{}{"command": "echo hi"},
			},
			wantTool: "bash",
			wantSess: "x",
			wantCmd:  "echo hi",
		},
		{
			name: "missing session defaults to unknown",
			raw: map[string]interface{}{
				"tool_name":  "Read",
				"tool_input": map[string]interface{}{"file_path": "/a/b"},
			},
			wantTool: "read",
			wantSess: "unknown",
			wantFile: "/a/b",
		},
		{
			name: "empty input is safe",
			raw: map[string]interface{}{
				"tool_name":  "Bash",
				"session_id": "s",
			},
			wantTool: "bash",
			wantSess: "s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, _ := json.Marshal(tt.raw)
			p, err := ParsePayload(bytes.NewReader(b))
			if err != nil {
				t.Fatalf("ParsePayload error: %v", err)
			}
			if p.ToolName != tt.wantTool {
				t.Errorf("ToolName = %q, want %q", p.ToolName, tt.wantTool)
			}
			if p.SessionID != tt.wantSess {
				t.Errorf("SessionID = %q, want %q", p.SessionID, tt.wantSess)
			}
			if tt.wantCmd != "" && p.Command != tt.wantCmd {
				t.Errorf("Command = %q, want %q", p.Command, tt.wantCmd)
			}
			if tt.wantFile != "" && p.FilePath != tt.wantFile {
				t.Errorf("FilePath = %q, want %q", p.FilePath, tt.wantFile)
			}
		})
	}
}

func TestParsePayload_InvalidJSON(t *testing.T) {
	_, err := ParsePayload(strings.NewReader("{not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParsePayload_NestedToolName(t *testing.T) {
	raw := map[string]interface{}{
		"tool_name": map[string]interface{}{
			"name": "Edit",
		},
		"session_id": "s1",
		"tool_input": map[string]interface{}{},
	}
	b, _ := json.Marshal(raw)
	p, err := ParsePayload(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("ParsePayload error: %v", err)
	}
	if p.ToolName != "edit" {
		t.Errorf("ToolName = %q, want %q", p.ToolName, "edit")
	}
}

// ---------------------------------------------------------------------------
// PatchPaths
// ---------------------------------------------------------------------------

func TestPatchPaths(t *testing.T) {
	tests := []struct {
		name      string
		patchText string
		want      []string
	}{
		{
			name:      "no patch text",
			patchText: "",
			want:      nil,
		},
		{
			name:      "single update file",
			patchText: "*** Update File: src/main.go\n@@ -1,3 +1,4 @@\n+import \"fmt\"",
			want:      []string{"src/main.go"},
		},
		{
			name: "multiple operations",
			patchText: "*** Add File: new.txt\n+content\n" +
				"*** Update File: existing.go\n@@ -1 +1 @@\n" +
				"*** Delete File: old.log\n",
			want: []string{"new.txt", "existing.go", "old.log"},
		},
		{
			name:      "no matching headers",
			patchText: "just some random text\nno patch here",
			want:      []string{}, // non-empty text still allocates empty slice
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{PatchText: tt.patchText}
			got := p.PatchPaths()
			if len(got) != len(tt.want) {
				t.Fatalf("PatchPaths() len = %d, want %d; got %v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("PatchPaths()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AllPaths
// ---------------------------------------------------------------------------

func TestAllPaths(t *testing.T) {
	tests := []struct {
		name      string
		filePath  string
		patchText string
		wantLen   int
	}{
		{"file_path only", "/a/b.go", "", 1},
		{"patch paths only", "", "*** Update File: x.go\n", 1},
		{"both", "/a/b.go", "*** Update File: x.go\n*** Add File: y.go\n", 3},
		{"neither", "", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{FilePath: tt.filePath, PatchText: tt.patchText}
			got := p.AllPaths()
			if len(got) != tt.wantLen {
				t.Errorf("AllPaths() len = %d, want %d; got %v", len(got), tt.wantLen, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// IsWriteLike
// ---------------------------------------------------------------------------

func TestIsWriteLike(t *testing.T) {
	for _, tc := range []struct {
		tool string
		want bool
	}{
		{"write", true}, {"edit", true}, {"apply_patch", true}, {"applypatch", true},
		{"bash", false}, {"read", false}, {"", false},
	} {
		p := &Payload{ToolName: tc.tool}
		if got := p.IsWriteLike(); got != tc.want {
			t.Errorf("IsWriteLike(%q) = %v, want %v", tc.tool, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// normalizeToolName
// ---------------------------------------------------------------------------

func TestNormalizeToolName(t *testing.T) {
	for _, tc := range []struct {
		input, want string
	}{
		{"Bash", "bash"},
		{"computer.Bash", "bash"},
		{"mcp.server.Read", "read"},
		{"  Write  ", "write"},
		{"", ""},
	} {
		if got := normalizeToolName(tc.input); got != tc.want {
			t.Errorf("normalizeToolName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Rule 8: Full test suite must use --parallel
// ---------------------------------------------------------------------------

func TestRule8_TestParallel(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocks  bool
	}{
		{"full suite without parallel blocks", "vendor/bin/pest", true},
		{"full suite with parallel but no processes blocks", "vendor/bin/pest --parallel", true},
		{"full suite correct", "vendor/bin/pest --parallel --processes=10", false},
		{"filtered test passes", `vendor/bin/pest --filter="UserTest"`, false},
		{"specific test file passes", "vendor/bin/pest tests/Feature/UserTest.php", false},
		{"herd coverage without parallel blocks", "herd coverage vendor/bin/pest --coverage", true},
		{"herd coverage correct", "herd coverage vendor/bin/pest --coverage --parallel --processes=10", false},
		{"artisan test without parallel blocks", "php artisan test", true},
		{"non-test command ignored", "echo hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{ToolName: "bash", Command: tt.command}
			state := newTempState(t)
			log := newTestLogger()

			blocked := didBlock(func() {
				rule8TestParallel(p, state, log)
			})
			if blocked != tt.blocks {
				t.Errorf("blocked = %v, want %v", blocked, tt.blocks)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rule 10: Block dangerous commands
// ---------------------------------------------------------------------------

func TestRule10_MigrateFresh_Blocked(t *testing.T) {
	p := &Payload{ToolName: "bash", Command: "php artisan migrate:fresh --seed"}
	state := newTempState(t)
	log := newTestLogger()

	if !didBlock(func() { rule10BlockDangerous(p, state, log) }) {
		t.Error("expected migrate:fresh to be blocked")
	}
}

func TestRule10_MigrateFresh_AllowedWithSkill(t *testing.T) {
	p := &Payload{ToolName: "bash", Command: "php artisan migrate:fresh --seed"}
	state := newTempState(t)
	state.Touch("skill-read-squash-migrations")
	log := newTestLogger()

	if didBlock(func() { rule10BlockDangerous(p, state, log) }) {
		t.Error("migrate:fresh should be allowed when squash-migrations skill is active")
	}
}

func TestRule10_AISignaturesInCommit(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocks  bool
	}{
		{
			"Co-Authored-By in commit",
			`git commit -m "feat: thing" -m "Co-Authored-By: Claude"`,
			true,
		},
		{
			"Generated by AI in commit",
			`git commit -m "fix: thing\n\nGenerated by AI"`,
			true,
		},
		{
			"normal commit passes",
			`git commit -m "feat: add new feature"`,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{ToolName: "bash", Command: tt.command}
			if didBlock(func() { rule10BlockDangerous(p, newTempState(t), newTestLogger()) }) != tt.blocks {
				t.Errorf("blocked mismatch for %s", tt.name)
			}
		})
	}
}

func TestRule10_AISignaturesInPR(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocks  bool
	}{
		{
			"Generated with Claude Code in PR",
			`gh pr create --title "feat" --body "Generated with Claude Code"`,
			true,
		},
		{
			"claude.com link in PR",
			`gh pr create --title "feat" --body "see claude.com/claude-code"`,
			true,
		},
		{
			"Co-Authored-By anthropic in PR edit",
			`gh pr edit --body "Co-Authored-By: noreply@anthropic.com"`,
			true,
		},
		{
			"robot emoji Generated in PR",
			"gh pr create --title \"feat\" --body \"\xf0\x9f\xa4\x96 Generated with Claude\"",
			true,
		},
		{
			"clean PR passes",
			`gh pr create --title "feat: add thing" --body "## Summary"`,
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{ToolName: "bash", Command: tt.command}
			if didBlock(func() { rule10BlockDangerous(p, newTempState(t), newTestLogger()) }) != tt.blocks {
				t.Errorf("blocked mismatch for %s", tt.name)
			}
		})
	}
}

func TestRule10_NonBashIgnored(t *testing.T) {
	p := &Payload{ToolName: "read", Command: "migrate:fresh"}
	if didBlock(func() { rule10BlockDangerous(p, newTempState(t), newTestLogger()) }) {
		t.Error("rule 10 should only apply to bash tool")
	}
}

// ---------------------------------------------------------------------------
// Rule 15: Backlog CLI enforcement
// ---------------------------------------------------------------------------

func TestRule15_BacklogCLI(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocks  bool
	}{
		{"grep backlog blocks", "grep -r status .planning/backlog/", true},
		{"sed backlog blocks", "sed -i 's/old/new/' .planning/backlog/item.md", true},
		{"awk backlog blocks", "awk '/status/' .planning/backlog/*.md", true},
		{"cat backlog blocks", "cat .planning/backlog/idea-001.md", true},
		{"for loop backlog blocks", "for f in .planning/backlog/*.md; do cat $f; done", true},
		{"bravros backlog passes", "bravros backlog --format table", false},
		{"unrelated grep passes", "grep -r TODO src/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{ToolName: "bash", Command: tt.command}
			if didBlock(func() { rule15BacklogCLI(p, newTempState(t), newTestLogger()) }) != tt.blocks {
				t.Errorf("blocked mismatch for %q", tt.command)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rule 16: Planning mv check
// ---------------------------------------------------------------------------

func TestRule16_PlanningMvCheck(t *testing.T) {
	// Set up a git repo with a tracked .planning file so bare mv is blocked
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	// Init git repo and track a planning file
	bash := func(cmd string) {
		parts := strings.Fields(cmd)
		c := exec.Command(parts[0], parts[1:]...)
		c.Dir = tmp
		_ = c.Run()
	}
	bash("git init")
	bash("git config user.email test@test.com")
	bash("git config user.name Test")
	os.MkdirAll(filepath.Join(tmp, ".planning"), 0755)
	os.WriteFile(filepath.Join(tmp, ".planning", "old.md"), []byte("content"), 0644)
	bash("git add .planning/old.md")
	bash("git commit -m 'add planning file'")

	tests := []struct {
		name    string
		command string
		blocks  bool
	}{
		{"bare mv on tracked file blocks", "mv .planning/old.md .planning/new.md", true},
		{"bare mv on untracked file passes", "mv .planning/untracked.md .planning/new.md", false},
		{"git mv passes", "git mv .planning/old.md .planning/new.md", false},
		{"mv on non-planning passes", "mv src/old.go src/new.go", false},
		{"chained git mv passes", "cd /project && git mv .planning/a.md .planning/b.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{ToolName: "bash", Command: tt.command}
			if didBlock(func() { rule16PlanningMvCheck(p, newTempState(t), newTestLogger()) }) != tt.blocks {
				t.Errorf("blocked mismatch for %q", tt.command)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Logger
// ---------------------------------------------------------------------------

func TestLogger_CreatesLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	logger := NewLogger("test-session-12345", "bash")
	defer logger.Close()

	logger.Log("test message")

	logDir := filepath.Join(tmpDir, ".claude", "hooks", "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("failed to read log dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one log file")
	}

	content, err := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}
	logStr := string(content)

	// Session ID truncated to 8 chars: "test-ses"
	if !strings.Contains(logStr, "[SESSION:test-ses]") {
		t.Errorf("log should contain truncated session ID, got: %s", logStr)
	}
	if !strings.Contains(logStr, "[bash]") {
		t.Errorf("log should contain tool name, got: %s", logStr)
	}
	if !strings.Contains(logStr, "test message") {
		t.Errorf("log should contain message, got: %s", logStr)
	}
}

func TestLogger_LogCall_Summary(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	logger := NewLogger("sess", "bash")
	defer logger.Close()

	logger.LogCall(map[string]interface{}{
		"command":     "ls -la",
		"description": "list files",
	})

	logDir := filepath.Join(tmpDir, ".claude", "hooks", "logs")
	entries, _ := os.ReadDir(logDir)
	content, _ := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	logStr := string(content)

	if !strings.Contains(logStr, "CALL |") {
		t.Errorf("expected CALL summary, got: %s", logStr)
	}
	if !strings.Contains(logStr, "description=list files") {
		t.Errorf("expected description in summary, got: %s", logStr)
	}
}

func TestLogger_LogCall_NoSummary(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	logger := NewLogger("s", "read")
	defer logger.Close()

	logger.LogCall(map[string]interface{}{
		"unknown_key": "value",
	})

	logDir := filepath.Join(tmpDir, ".claude", "hooks", "logs")
	entries, _ := os.ReadDir(logDir)
	content, _ := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	logStr := string(content)

	if !strings.Contains(logStr, "no-summary") {
		t.Errorf("expected no-summary fallback, got: %s", logStr)
	}
}

func TestLogger_LogCall_TruncatesLongValues(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	logger := NewLogger("s", "bash")
	defer logger.Close()

	longCmd := strings.Repeat("x", 200)
	logger.LogCall(map[string]interface{}{"command": longCmd})

	logDir := filepath.Join(tmpDir, ".claude", "hooks", "logs")
	entries, _ := os.ReadDir(logDir)
	content, _ := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	logStr := string(content)

	// Value should be truncated to 80 chars
	if strings.Contains(logStr, longCmd) {
		t.Error("expected long value to be truncated")
	}
	if !strings.Contains(logStr, "command="+strings.Repeat("x", 80)) {
		t.Errorf("expected 80-char truncated value in: %s", logStr)
	}
}

// ---------------------------------------------------------------------------
// SessionState
// ---------------------------------------------------------------------------

func TestSessionState_BasicOps(t *testing.T) {
	state := newTempState(t)

	// Touch + Exists
	if state.Exists("marker") {
		t.Error("marker should not exist yet")
	}
	state.Touch("marker")
	if !state.Exists("marker") {
		t.Error("marker should exist after Touch")
	}

	// WriteText + ReadText
	state.WriteText("key", "value")
	if got := state.ReadText("key"); got != "value" {
		t.Errorf("ReadText = %q, want %q", got, "value")
	}

	// ReadText on missing file returns ""
	if got := state.ReadText("missing"); got != "" {
		t.Errorf("ReadText(missing) = %q, want empty", got)
	}

	// WriteInt + ReadInt
	state.WriteInt("counter", 5)
	if got := state.ReadInt("counter"); got != 5 {
		t.Errorf("ReadInt = %d, want 5", got)
	}

	// ReadInt on missing returns 0
	if got := state.ReadInt("missing"); got != 0 {
		t.Errorf("ReadInt(missing) = %d, want 0", got)
	}

	// IncrInt
	got := state.IncrInt("counter")
	if got != 6 {
		t.Errorf("IncrInt = %d, want 6", got)
	}

	// AppendLine
	state.AppendLine("lines", "first")
	state.AppendLine("lines", "second")
	text := state.ReadText("lines")
	if !strings.Contains(text, "first") || !strings.Contains(text, "second") {
		t.Errorf("AppendLine content = %q, want both lines", text)
	}
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

func TestLoadBravrosConfig_Default(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	cfg, found := LoadBravrosConfig()
	if found {
		t.Error("expected found=false when file missing")
	}
	if cfg.StagingBranch != "homolog" {
		t.Errorf("StagingBranch = %q, want %q", cfg.StagingBranch, "homolog")
	}
}

func TestLoadBravrosConfig_CustomBranch(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	os.WriteFile(".bravros.yml", []byte("staging_branch: staging\n"), 0644)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Error("expected found=true")
	}
	if cfg.StagingBranch != "staging" {
		t.Errorf("StagingBranch = %q, want %q", cfg.StagingBranch, "staging")
	}
}

func TestLoadBravrosConfig_EmptyBranchDefaultsToHomolog(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	os.WriteFile(".bravros.yml", []byte("staging_branch: \"\"\n"), 0644)

	cfg, found := LoadBravrosConfig()
	if !found {
		t.Error("expected found=true")
	}
	if cfg.StagingBranch != "homolog" {
		t.Errorf("StagingBranch = %q, want %q", cfg.StagingBranch, "homolog")
	}
}

func TestLoadBravrosConfig_InvalidYAML(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	os.WriteFile(".bravros.yml", []byte("{{invalid yaml"), 0644)

	cfg, found := LoadBravrosConfig()
	if found {
		t.Error("expected found=false for invalid YAML")
	}
	if cfg.StagingBranch != "homolog" {
		t.Errorf("StagingBranch = %q, want default %q", cfg.StagingBranch, "homolog")
	}
}

// ---------------------------------------------------------------------------
// isAutonomousPipeline
// ---------------------------------------------------------------------------

func TestIsAutonomousPipeline(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want bool
	}{
		{"auto-merge", true},
		{"auto-pr", true},
		{"auto-pr-wt", true},
		{"finish", false},
		{"pr", false},
		{"", false},
	} {
		if got := isAutonomousPipeline(tc.cmd); got != tc.want {
			t.Errorf("isAutonomousPipeline(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// isAllowedMergeContext
// ---------------------------------------------------------------------------

func TestIsAllowedMergeContext(t *testing.T) {
	tests := []struct {
		name       string
		activeCmd  string
		skillReads []string
		want       bool
	}{
		{"finish context", "finish", nil, true},
		{"hotfix context", "hotfix", nil, true},
		{"auto-merge context blocked", "auto-merge", nil, false},
		{"auto-pr context blocked", "auto-pr", nil, false},
		{"pr context blocked", "pr", nil, false},
		{"empty context blocked", "", nil, false},
		{"fallback finish skill read", "address-pr", []string{"skill-read-finish"}, true},
		{"fallback hotfix skill read", "address-pr", []string{"skill-read-hotfix"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newTempState(t)
			if tt.activeCmd != "" {
				state.WriteText("active-command", tt.activeCmd)
			}
			for _, sr := range tt.skillReads {
				state.Touch(sr)
			}
			if got := isAllowedMergeContext(state); got != tt.want {
				t.Errorf("isAllowedMergeContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractPRNumber
// ---------------------------------------------------------------------------

func TestExtractPRNumber(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    string
	}{
		{"gh pr merge 42", "42"},
		{"gh pr merge 123 --squash", "123"},
		{"gh api repos/owner/repo/pulls/99/merge -X PUT", "99"},
		{"gh pr create", ""},
		{"echo hello", ""},
	} {
		if got := extractPRNumber(tc.command); got != tc.want {
			t.Errorf("extractPRNumber(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Rule 18: Project config detection — recommend detect-stack --write
// ---------------------------------------------------------------------------

func TestRule18_MissingConfig_RecommendsDetectStack(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	// No .bravros.yml in tmp dir
	p := &Payload{ToolName: "bash", Command: "echo hello"}
	state := newTempState(t)

	// Capture log output — rule 18 calls log.Warn, not log.Block
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	log := NewLogger("test", "bash")
	defer log.Close()

	rule18ProjectConfig(p, state, log)

	// Read log file to verify message content
	logDir := filepath.Join(tmpHome, ".claude", "hooks", "logs")
	entries, err := os.ReadDir(logDir)
	if err != nil || len(entries) == 0 {
		t.Fatal("expected log file to be created")
	}
	content, _ := os.ReadFile(filepath.Join(logDir, entries[0].Name()))
	logStr := string(content)

	if !strings.Contains(logStr, "bravros detect-stack --write") {
		t.Errorf("expected log to recommend 'bravros detect-stack --write', got: %s", logStr)
	}
}

// ---------------------------------------------------------------------------
// Rule 10: Destructive DB commands blocked when deployed
// ---------------------------------------------------------------------------

func TestRule10_DeployedBlocks(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocks  bool
	}{
		{"migrate:rollback blocked", "php artisan migrate:rollback", true},
		{"db:wipe blocked", "php artisan db:wipe --force", true},
		{"DROP TABLE blocked", "mysql -e 'DROP TABLE users;'", true},
		{"TRUNCATE TABLE blocked", `mysql -e "TRUNCATE TABLE sessions;"`, true},
		{"DELETE FROM without WHERE blocked", `mysql -e "DELETE FROM users;"`, true},
		{"normal migrate allowed", "php artisan migrate", false},
		{"SELECT allowed", `mysql -e "SELECT * FROM users WHERE id=1;"`, false},
		{"DELETE with WHERE allowed", `mysql -e "DELETE FROM users WHERE id=1;"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig, _ := os.Getwd()
			tmp := t.TempDir()
			os.Chdir(tmp)
			defer os.Chdir(orig)

			// Create .bravros.yml with deployed: true
			os.WriteFile(".bravros.yml", []byte("env:\n  deployed: true\n"), 0644)

			p := &Payload{ToolName: "bash", Command: tt.command}
			state := newTempState(t)
			log := newTestLogger()

			blocked := didBlock(func() {
				rule10BlockDangerous(p, state, log)
			})
			if blocked != tt.blocks {
				t.Errorf("blocked = %v, want %v for command %q", blocked, tt.blocks, tt.command)
			}
		})
	}
}

func TestRule10_NotDeployedAllows(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"migrate:rollback allowed", "php artisan migrate:rollback"},
		{"db:wipe allowed", "php artisan db:wipe --force"},
		{"DROP TABLE allowed", "mysql -e 'DROP TABLE users;'"},
		{"TRUNCATE TABLE allowed", `mysql -e "TRUNCATE TABLE sessions;"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig, _ := os.Getwd()
			tmp := t.TempDir()
			os.Chdir(tmp)
			defer os.Chdir(orig)

			// Create .bravros.yml with deployed: false
			os.WriteFile(".bravros.yml", []byte("env:\n  deployed: false\n"), 0644)

			p := &Payload{ToolName: "bash", Command: tt.command}
			state := newTempState(t)
			log := newTestLogger()

			if didBlock(func() { rule10BlockDangerous(p, state, log) }) {
				t.Errorf("should NOT block %q when deployed: false", tt.command)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rule 19: Agent model enforcement
// ---------------------------------------------------------------------------

func TestRule19_AgentModelEnforcement(t *testing.T) {
	tests := []struct {
		name   string
		tool   string
		prompt string
		model  string
		blocks bool
	}{
		{
			name:   "Agent+[H]+haiku → ALLOW",
			tool:   "agent",
			prompt: "Do the [H] tasks here",
			model:  "haiku",
			blocks: false,
		},
		{
			name:   "Agent+[H]+sonnet → BLOCK (wrong model)",
			tool:   "agent",
			prompt: "Do the [H] tasks here",
			model:  "sonnet",
			blocks: true,
		},
		{
			name:   "Agent+[S]+sonnet → ALLOW",
			tool:   "agent",
			prompt: "Implement [S] business logic",
			model:  "sonnet",
			blocks: false,
		},
		{
			name:   "Agent+[S]+haiku → BLOCK (too weak)",
			tool:   "agent",
			prompt: "Implement [S] business logic",
			model:  "haiku",
			blocks: true,
		},
		{
			name:   "Agent+[O]+opus → ALLOW",
			tool:   "agent",
			prompt: "Architecture decision [O] required",
			model:  "opus",
			blocks: false,
		},
		{
			name:   "Agent+[O]+sonnet → BLOCK (wrong model)",
			tool:   "agent",
			prompt: "Architecture decision [O] required",
			model:  "sonnet",
			blocks: true,
		},
		{
			name:   "Agent+[H]+[S]+sonnet → ALLOW (highest wins)",
			tool:   "agent",
			prompt: "Run [H] config step then [S] logic step",
			model:  "sonnet",
			blocks: false,
		},
		{
			name:   "Agent+markers+no model → BLOCK",
			tool:   "agent",
			prompt: "Do [S] tasks",
			model:  "",
			blocks: true,
		},
		{
			name:   "Agent+no markers → ALLOW (free pass)",
			tool:   "agent",
			prompt: "Just do something without markers",
			model:  "",
			blocks: false,
		},
		{
			name:   "Non-agent tool → ALLOW (skip)",
			tool:   "bash",
			prompt: "Do [S] tasks",
			model:  "",
			blocks: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{
				ToolName: tt.tool,
				Input:    map[string]interface{}{"prompt": tt.prompt, "model": tt.model},
			}
			state := newTempState(t)
			log := newTestLogger()

			blocked := didBlock(func() {
				rule19AgentModelEnforcement(p, state, log)
			})
			if blocked != tt.blocks {
				t.Errorf("blocked = %v, want %v", blocked, tt.blocks)
			}
		})
	}
}

func TestRule10_NoConfigAllows(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	// No .bravros.yml — destructive commands (other than migrate:fresh) should be allowed
	p := &Payload{ToolName: "bash", Command: "php artisan migrate:rollback"}
	state := newTempState(t)
	log := newTestLogger()

	if didBlock(func() { rule10BlockDangerous(p, state, log) }) {
		t.Error("should NOT block migrate:rollback when no .bravros.yml exists")
	}
}

// ---------------------------------------------------------------------------
// Rule 1 + Rule 2: skill alias tests (taste-skill / brand-guidelines → frontend-design)
// ---------------------------------------------------------------------------

func TestRule1_TasteSkillSatisfiesFrontendDesign(t *testing.T) {
	state := newTempState(t)
	log := newTestLogger()

	// Invoke taste-skill — this should satisfy frontend-design requirement
	skillPayload := &Payload{
		ToolName: "skill",
		Input:    map[string]interface{}{"skill": "taste-skill"},
	}
	rule2TrackSkillReads(skillPayload, state, log)

	// Now attempt to write a .css file — should NOT be blocked
	writePayload := &Payload{
		ToolName: "write",
		FilePath: "styles/main.css",
	}
	if didBlock(func() { rule1SkillReadBeforeFrontend(writePayload, state, log, writePayload.AllPaths()) }) {
		t.Error("taste-skill alias should satisfy frontend-design requirement for .css write")
	}
}

func TestRule1_BrandGuidelinesSatisfiesFrontendDesign(t *testing.T) {
	state := newTempState(t)
	log := newTestLogger()

	// Invoke brand-guidelines — this should satisfy frontend-design requirement
	skillPayload := &Payload{
		ToolName: "skill",
		Input:    map[string]interface{}{"skill": "brand-guidelines"},
	}
	rule2TrackSkillReads(skillPayload, state, log)

	// Now attempt to write an .html file — should NOT be blocked
	writePayload := &Payload{
		ToolName: "write",
		FilePath: "index.html",
	}
	if didBlock(func() { rule1SkillReadBeforeFrontend(writePayload, state, log, writePayload.AllPaths()) }) {
		t.Error("brand-guidelines alias should satisfy frontend-design requirement for .html write")
	}
}

func TestRule1_FrontendWriteBlockedWithoutSkillRead(t *testing.T) {
	state := newTempState(t)
	log := newTestLogger()

	// No skill invoked — writing .jsx should be blocked
	writePayload := &Payload{
		ToolName: "write",
		FilePath: "App.jsx",
	}
	if !didBlock(func() { rule1SkillReadBeforeFrontend(writePayload, state, log, writePayload.AllPaths()) }) {
		t.Error("should block .jsx write when no frontend-design skill has been read")
	}
}

func TestRule1_DirectFrontendDesignSkillSatisfies(t *testing.T) {
	state := newTempState(t)
	log := newTestLogger()

	// Invoke frontend-design directly
	skillPayload := &Payload{
		ToolName: "skill",
		Input:    map[string]interface{}{"skill": "frontend-design"},
	}
	rule2TrackSkillReads(skillPayload, state, log)

	// Now attempt to write a .tsx file — should NOT be blocked
	writePayload := &Payload{
		ToolName: "write",
		FilePath: "components/Button.tsx",
	}
	if didBlock(func() { rule1SkillReadBeforeFrontend(writePayload, state, log, writePayload.AllPaths()) }) {
		t.Error("frontend-design skill invocation should satisfy frontend-design requirement for .tsx write")
	}
}

// ---------------------------------------------------------------------------
// isSDLCFlow helper
// ---------------------------------------------------------------------------

func TestIsSDLCFlow(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want bool
	}{
		{"plan", true},
		{"plan-review", true},
		{"plan-approved", true},
		{"plan-check", true},
		{"flow", true},
		{"auto-pr", true},
		{"auto-pr-wt", true},
		{"auto-merge", true},
		{"hotfix", false},
		{"finish", false},
		{"debug", false},
		{"pr", false},
		{"", false},
	} {
		if got := isSDLCFlow(tc.cmd); got != tc.want {
			t.Errorf("isSDLCFlow(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Rule 6: reports exclusion
// ---------------------------------------------------------------------------

func TestRule6_ReportsExclusion(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	tests := []struct {
		name   string
		path   string
		blocks bool
	}{
		{"regular plan file blocks", ".planning/0031-test-todo.md", true},
		{"backlog excluded", ".planning/backlog/B-0001-test.md", false},
		{"reports excluded", ".planning/reports/R-0001-test-open.md", false},
		{"user-reports excluded", ".planning/user-reports/U-0001-test.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{ToolName: "write", FilePath: tt.path}
			state := newTempState(t)
			log := newTestLogger()

			blocked := didBlock(func() {
				rule6PlanTemplateRead(p, state, log, p.AllPaths())
			})
			if blocked != tt.blocks {
				t.Errorf("blocked = %v, want %v for path %s", blocked, tt.blocks, tt.path)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rule 8: Laravel-only
// ---------------------------------------------------------------------------

func TestRule8_NonLaravelSkips(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	// Create .bravros.yml with non-Laravel framework
	os.WriteFile(".bravros.yml", []byte("stack:\n  framework: nextjs\n"), 0644)

	p := &Payload{ToolName: "bash", Command: "vendor/bin/pest"}
	state := newTempState(t)
	log := newTestLogger()

	if didBlock(func() { rule8TestParallel(p, state, log) }) {
		t.Error("rule8 should NOT fire for non-Laravel projects")
	}
}

func TestRule8_LaravelStillBlocks(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	// Create .bravros.yml with Laravel
	os.WriteFile(".bravros.yml", []byte("stack:\n  framework: laravel\n"), 0644)

	p := &Payload{ToolName: "bash", Command: "vendor/bin/pest"}
	state := newTempState(t)
	log := newTestLogger()

	if !didBlock(func() { rule8TestParallel(p, state, log) }) {
		t.Error("rule8 should block full suite without parallel for Laravel")
	}
}

// ---------------------------------------------------------------------------
// Rule 12: SDLC flow block vs ad-hoc warn
// ---------------------------------------------------------------------------

func TestRule12_SDLCFlowBlocks(t *testing.T) {
	p := &Payload{ToolName: "bash", Command: "gh pr create --title test"}
	state := newTempState(t)
	// Set up plan-approved checkpoint without plan-check
	state.AppendLine("checkpoints.txt", "plan-approved:1")
	state.WriteText("active-command", "auto-pr")
	log := newTestLogger()

	if !didBlock(func() { rule12PlanCheckSkip(p, state, log) }) {
		t.Error("rule12 should BLOCK PR creation in SDLC flow without plan-check")
	}
}

func TestRule12_AdHocWarns(t *testing.T) {
	p := &Payload{ToolName: "bash", Command: "gh pr create --title test"}
	state := newTempState(t)
	state.AppendLine("checkpoints.txt", "plan-approved:1")
	state.WriteText("active-command", "pr") // not an SDLC flow
	log := newTestLogger()

	if didBlock(func() { rule12PlanCheckSkip(p, state, log) }) {
		t.Error("rule12 should only WARN in ad-hoc mode, not block")
	}
}

// ---------------------------------------------------------------------------
// Rule 13: SDLC flow block vs ad-hoc warn
// ---------------------------------------------------------------------------

func TestRule13_SDLCFlowBlocks(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	// Create a plan file with unchecked acceptance criteria
	os.MkdirAll(".planning", 0755)
	os.WriteFile(".planning/0031-test-todo.md", []byte("## Acceptance Criteria\n\n- [ ] First criterion\n- [x] Second criterion\n"), 0644)

	p := &Payload{ToolName: "bash", Command: "gh pr create --title test"}
	state := newTempState(t)
	state.WriteText("active-command", "plan-approved")
	log := newTestLogger()

	if !didBlock(func() { rule13UncheckedAcceptance(p, state, log) }) {
		t.Error("rule13 should BLOCK in SDLC flow with unchecked acceptance criteria")
	}
}

func TestRule13_AdHocWarns(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	os.MkdirAll(".planning", 0755)
	os.WriteFile(".planning/0031-test-todo.md", []byte("## Acceptance Criteria\n\n- [ ] First criterion\n"), 0644)

	p := &Payload{ToolName: "bash", Command: "gh pr create --title test"}
	state := newTempState(t)
	state.WriteText("active-command", "quick") // not SDLC flow
	log := newTestLogger()

	if didBlock(func() { rule13UncheckedAcceptance(p, state, log) }) {
		t.Error("rule13 should only WARN in ad-hoc mode, not block")
	}
}

// ---------------------------------------------------------------------------
// Rule 16: untracked file skip
// ---------------------------------------------------------------------------

func TestRule16_UntrackedAllowsBareMv(t *testing.T) {
	// Verify the extractMvSource helper works correctly
	src := extractMvSource("mv .planning/old.md .planning/new.md")
	if src != ".planning/old.md" {
		t.Errorf("extractMvSource = %q, want .planning/old.md", src)
	}
}

func TestExtractMvSource(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    string
	}{
		{"mv .planning/old.md .planning/new.md", ".planning/old.md"},
		{"mv src/old.go src/new.go", "src/old.go"},
		{"git mv .planning/old.md .planning/new.md", ".planning/old.md"},
		{"echo hello", ""},
	} {
		if got := extractMvSource(tc.command); got != tc.want {
			t.Errorf("extractMvSource(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Rule 17: autonomous deny main
// ---------------------------------------------------------------------------

func TestRule17_AutonomousDenyMain(t *testing.T) {
	state := newTempState(t)
	state.WriteText("active-command", "auto-pr")
	if isAllowedMergeContext(state) {
		t.Error("autonomous pipeline should NOT be allowed to merge to main")
	}
}

func TestRule17_WasAutonomousStickyDeny(t *testing.T) {
	state := newTempState(t)
	state.Touch("was-autonomous")
	state.WriteText("active-command", "finish")
	if isAllowedMergeContext(state) {
		t.Error("was-autonomous sticky state should block merge even in finish context")
	}
}

func TestRule17_FinishWithoutAutonomousAllows(t *testing.T) {
	state := newTempState(t)
	state.WriteText("active-command", "finish")
	if !isAllowedMergeContext(state) {
		t.Error("finish without autonomous history should allow merge to main")
	}
}

// ---------------------------------------------------------------------------
// Rule 20: bash redirect block
// ---------------------------------------------------------------------------

func TestRule20_BashRedirectBlock(t *testing.T) {
	tests := []struct {
		name    string
		command string
		blocks  bool
	}{
		{"cat heredoc redirect blocks", "cat > .planning/0031-test-todo.md << 'EOF'", true},
		{"echo redirect blocks", "echo content > .planning/test.md", true},
		{"tee blocks", "tee .planning/test.md", true},
		{"backlog excluded", "cat > .planning/backlog/test.md << 'EOF'", false},
		{"reports excluded", "cat > .planning/reports/R-0001.md << 'EOF'", false},
		{"user-reports excluded", "echo test > .planning/user-reports/U-0001.md", false},
		{"unrelated passes", "cat > /tmp/test.md << 'EOF'", false},
		{"template read allows", "cat > .planning/0031-test-todo.md << 'EOF'", false}, // when template read
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{ToolName: "bash", Command: tt.command}
			state := newTempState(t)
			if tt.name == "template read allows" {
				state.Touch("read-plan-template")
			}
			log := newTestLogger()

			blocked := didBlock(func() {
				rule20BashRedirectToPlanning(p, state, log)
			})
			if blocked != tt.blocks {
				t.Errorf("blocked = %v, want %v for %q", blocked, tt.blocks, tt.command)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rule 21: auto skill gate
// ---------------------------------------------------------------------------

func TestRule21_AutoSkillGate(t *testing.T) {
	state := newTempState(t)
	log := newTestLogger()

	// Non-auto skill — should not track
	p := &Payload{ToolName: "skill", Input: map[string]interface{}{"skill": "plan"}}
	rule21AutoSkillGate(p, state, log)
	if state.Exists("auto-skill-invoked-plan") {
		t.Error("non-auto skill should not be tracked")
	}

	// Auto skill — should track
	p2 := &Payload{ToolName: "skill", Input: map[string]interface{}{"skill": "auto-pr"}}
	rule21AutoSkillGate(p2, state, log)
	if !state.Exists("auto-skill-invoked-auto-pr") {
		t.Error("auto-pr invocation should be tracked in state")
	}
}

func TestRule21_LockFileReentry(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	// Create lock file to simulate re-entry
	os.MkdirAll(".planning", 0755)
	os.WriteFile(".planning/.auto-pr-lock", []byte("test"), 0644)

	state := newTempState(t)
	log := newTestLogger()

	p := &Payload{ToolName: "skill", Input: map[string]interface{}{"skill": "auto-pr"}}
	rule21AutoSkillGate(p, state, log)

	// Should NOT track when lock file exists (it's a re-entry)
	if state.Exists("auto-skill-invoked-auto-pr") {
		t.Error("re-entry should not track auto-skill-invoked state when lock file exists")
	}
}

// ---------------------------------------------------------------------------
// Rule 21b: lock file tamper
// ---------------------------------------------------------------------------

func TestRule21b_LockTamperBlock(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		command string
		path    string
		blocks  bool
	}{
		{"rm lock bash", "bash", "rm .planning/.auto-pr-lock", "", true},
		{"rm lock bash alt", "bash", "rm .auto-pr-lock", "", true},
		{"redirect lock", "bash", "cat /dev/null > .planning/.auto-pr-lock", "", true},
		{"truncate lock", "bash", "truncate -s 0 .planning/.auto-pr-lock", "", true},
		{"write lock file", "write", "", ".planning/.auto-pr-lock", true},
		{"edit lock file", "edit", "", "something/.auto-pr-lock", true},
		{"unrelated rm", "bash", "rm .planning/test.md", "", false},
		{"unrelated write", "write", "", ".planning/test.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{ToolName: tt.tool, Command: tt.command, FilePath: tt.path}
			state := newTempState(t)
			log := newTestLogger()

			blocked := didBlock(func() {
				rule21bLockFileTamper(p, state, log)
			})
			if blocked != tt.blocks {
				t.Errorf("blocked = %v, want %v", blocked, tt.blocks)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rule 21c: autonomous mode tracking
// ---------------------------------------------------------------------------

func TestRule21c_StickyTracking(t *testing.T) {
	orig, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(orig)

	state := newTempState(t)
	log := newTestLogger()

	// No lock file — should not set was-autonomous
	p := &Payload{ToolName: "bash", Command: "echo test"}
	rule21cTrackAutonomousMode(p, state, log)
	if state.Exists("was-autonomous") {
		t.Error("should not set was-autonomous without lock file")
	}

	// Create lock file
	os.MkdirAll(".planning", 0755)
	os.WriteFile(".planning/.auto-pr-lock", []byte("test"), 0644)

	rule21cTrackAutonomousMode(p, state, log)
	if !state.Exists("was-autonomous") {
		t.Error("should set was-autonomous when lock file exists")
	}

	// Remove lock file — state should persist
	os.Remove(".planning/.auto-pr-lock")
	if !state.Exists("was-autonomous") {
		t.Error("was-autonomous should be sticky even after lock removal")
	}
}

// ---------------------------------------------------------------------------
// Rule 22: debug read-only
// ---------------------------------------------------------------------------

func TestRule22_DebugReadOnly(t *testing.T) {
	tests := []struct {
		name      string
		tool      string
		path      string
		activeCmd string
		blocks    bool
	}{
		{"edit source during debug blocks", "edit", "app/Models/User.php", "debug", true},
		{"write source during debug blocks", "write", "src/main.go", "debug", true},
		{"write .planning/ during debug allowed", "write", ".planning/reports/R-0001.md", "debug", false},
		{"edit during non-debug allowed", "edit", "app/Models/User.php", "plan", false},
		{"read during debug allowed", "read", "app/Models/User.php", "debug", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{ToolName: tt.tool, FilePath: tt.path}
			state := newTempState(t)
			state.WriteText("active-command", tt.activeCmd)
			log := newTestLogger()

			blocked := didBlock(func() {
				rule22ReadOnlySkillEnforcement(p, state, log)
			})
			if blocked != tt.blocks {
				t.Errorf("blocked = %v, want %v", blocked, tt.blocks)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rule 23: block deployed dir writes
// ---------------------------------------------------------------------------

func TestRule23_BlockDeployedDirWrites(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	tests := []struct {
		name      string
		tool      string
		path      string
		activeCmd string
		blocks    bool
	}{
		{"skills write blocks", "write", filepath.Join(homeDir, ".claude/skills/test/SKILL.md"), "", true},
		{"hooks write blocks", "edit", filepath.Join(homeDir, ".claude/hooks/pre-tool-use.sh"), "", true},
		{"hooks logs allowed", "write", filepath.Join(homeDir, ".claude/hooks/logs/2026-01-01.log"), "", false},
		{"settings.json allowed", "write", filepath.Join(homeDir, ".claude/settings.json"), "", false},
		{"bin/bravros allowed", "write", filepath.Join(homeDir, ".claude/bin/bravros"), "", false},
		{"projects/ allowed", "write", filepath.Join(homeDir, ".claude/projects/test/memory.md"), "", false},
		{"verify-install exempt", "write", filepath.Join(homeDir, ".claude/skills/test/SKILL.md"), "verify-install", false},
		{"non-deployed path passes", "write", "/tmp/test.md", "", false},
		{"read tool passes", "read", filepath.Join(homeDir, ".claude/skills/test/SKILL.md"), "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Payload{ToolName: tt.tool, FilePath: tt.path}
			state := newTempState(t)
			if tt.activeCmd != "" {
				state.WriteText("active-command", tt.activeCmd)
			}
			log := newTestLogger()

			blocked := didBlock(func() {
				rule23BlockDeployedDirWrites(p, state, log)
			})
			if blocked != tt.blocks {
				t.Errorf("blocked = %v, want %v for %s", blocked, tt.blocks, tt.path)
			}
		})
	}
}
