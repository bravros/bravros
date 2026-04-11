package cmd

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─── Phase 1: JSON parsing tests ───────────────────────────────────────────────

func TestStatuslineJSONParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantDir  string
	}{
		{
			name:     "valid full input",
			input:    `{"model":{"display_name":"Opus 4.6 (1M context)"},"workspace":{"current_dir":"/tmp/test","project_dir":"/tmp"}}`,
			wantName: "Opus 4.6 (1M context)",
			wantDir:  "/tmp/test",
		},
		{
			name:     "empty object",
			input:    `{}`,
			wantName: "",
			wantDir:  "",
		},
		{
			name:     "partial fields",
			input:    `{"model":{"display_name":"Test"}}`,
			wantName: "Test",
			wantDir:  "",
		},
		{
			name:     "null values in JSON",
			input:    `{"model":null,"workspace":null}`,
			wantName: "",
			wantDir:  "",
		},
		{
			name:     "missing nested fields",
			input:    `{"model":{}}`,
			wantName: "",
			wantDir:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input statuslineInput
			err := json.Unmarshal([]byte(tt.input), &input)
			if err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}
			if input.Model.DisplayName != tt.wantName {
				t.Errorf("DisplayName = %q, want %q", input.Model.DisplayName, tt.wantName)
			}
			if input.Workspace.CurrentDir != tt.wantDir {
				t.Errorf("CurrentDir = %q, want %q", input.Workspace.CurrentDir, tt.wantDir)
			}
		})
	}
}

func TestStatuslineJSONParsingTokens(t *testing.T) {
	input := `{
		"context_window": {
			"current_usage": {
				"input_tokens": 100000,
				"cache_creation_input_tokens": 50000,
				"cache_read_input_tokens": 25000
			},
			"context_window_size": 1000000
		},
		"cost": {"total_duration_ms": 300000},
		"exceeds_200k_tokens": true
	}`

	var parsed statuslineInput
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if parsed.ContextWindow.CurrentUsage.InputTokens != 100000 {
		t.Errorf("InputTokens = %d, want 100000", parsed.ContextWindow.CurrentUsage.InputTokens)
	}
	if parsed.ContextWindow.CurrentUsage.CacheCreationInputTokens != 50000 {
		t.Errorf("CacheCreationInputTokens = %d, want 50000", parsed.ContextWindow.CurrentUsage.CacheCreationInputTokens)
	}
	if parsed.ContextWindow.CurrentUsage.CacheReadInputTokens != 25000 {
		t.Errorf("CacheReadInputTokens = %d, want 25000", parsed.ContextWindow.CurrentUsage.CacheReadInputTokens)
	}
	if parsed.ContextWindow.ContextWindowSize != 1000000 {
		t.Errorf("ContextWindowSize = %d, want 1000000", parsed.ContextWindow.ContextWindowSize)
	}
	if !parsed.Exceeds200K {
		t.Error("Exceeds200K should be true")
	}
}

// ─── Phase 2: Context window + duration tests ──────────────────────────────────

func TestCalcTokenUsage(t *testing.T) {
	tests := []struct {
		name  string
		usage statuslineUsage
		want  int64
	}{
		{"all fields", statuslineUsage{100, 50, 25}, 175},
		{"zeros", statuslineUsage{0, 0, 0}, 0},
		{"only input", statuslineUsage{500000, 0, 0}, 500000},
		{"large values", statuslineUsage{500000, 200000, 300000}, 1000000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcTokenUsage(tt.usage)
			if got != tt.want {
				t.Errorf("calcTokenUsage() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCalcPercentage(t *testing.T) {
	tests := []struct {
		name    string
		current int64
		size    int64
		want    int
	}{
		{"6 percent", 60000, 1000000, 6},
		{"50 percent", 500000, 1000000, 50},
		{"100 percent", 1000000, 1000000, 100},
		{"0 percent", 0, 1000000, 0},
		{"zero size", 100, 0, 0},
		{"negative size", 100, -1, 0},
		{"over 100", 1500000, 1000000, 150},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcPercentage(tt.current, tt.size)
			if got != tt.want {
				t.Errorf("calcPercentage(%d, %d) = %d, want %d", tt.current, tt.size, got, tt.want)
			}
		})
	}
}

func TestBuildProgressBar(t *testing.T) {
	tests := []struct {
		name string
		pct  int
		want string
	}{
		{"0 percent", 0, "░░░░░░░░░░"},
		{"10 percent", 10, "█░░░░░░░░░"},
		{"50 percent", 50, "█████░░░░░"},
		{"100 percent", 100, "██████████"},
		{"6 percent", 6, "░░░░░░░░░░"}, // 6*10/100 = 0
		{"60 percent", 60, "██████░░░░"},
		{"over 100", 150, "██████████"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildProgressBar(tt.pct)
			if got != tt.want {
				t.Errorf("buildProgressBar(%d) = %q, want %q", tt.pct, got, tt.want)
			}
		})
	}
}

func TestBarColor(t *testing.T) {
	tests := []struct {
		name string
		pct  int
		want string
	}{
		{"green at 0", 0, ansiGreen},
		{"green at 50", 50, ansiGreen},
		{"yellow at 51", 51, ansiYellow},
		{"yellow at 65", 65, ansiYellow},
		{"red at 66", 66, ansiRed},
		{"red at 100", 100, ansiRed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := barColor(tt.pct)
			if got != tt.want {
				t.Errorf("barColor(%d) = %q, want %q", tt.pct, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		ms   int64
		want string
	}{
		{"zero", 0, ""},
		{"negative", -1000, ""},
		{"30 seconds", 30000, ""},
		{"4 minutes", 240000, "4m"},
		{"1 hour 1 min", 3660000, "1h1m"},
		{"2 hours 30 min", 9000000, "2h30m"},
		{"exactly 1 hour", 3600000, "1h0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.ms)
			if got != tt.want {
				t.Errorf("formatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
			}
		})
	}
}

func TestCalcRemainingTime(t *testing.T) {
	tests := []struct {
		name       string
		current    int64
		size       int64
		durationMs int64
		want       string
	}{
		{"no duration", 100000, 1000000, 0, ""},
		{"no tokens", 0, 1000000, 300000, ""},
		{"too early", 100000, 1000000, 30000, ""},
		{"6 percent 4m in", 60000, 1000000, 240000, "~1h2m left"},
		{"50 percent 10m in", 500000, 1000000, 600000, "~10m left"},
		{"near full", 990000, 1000000, 600000, ""}, // ~6 seconds, no display
		{"zero size", 100000, 0, 300000, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calcRemainingTime(tt.current, tt.size, tt.durationMs)
			if got != tt.want {
				t.Errorf("calcRemainingTime(%d, %d, %d) = %q, want %q", tt.current, tt.size, tt.durationMs, got, tt.want)
			}
		})
	}
}

// ─── Phase 4: Folder display + promo tests ─────────────────────────────────────

func TestFolderDisplay(t *testing.T) {
	tests := []struct {
		name       string
		currentDir string
		projectDir string
		want       string
	}{
		{"same dir", "/Users/me/Sites/app", "/Users/me/Sites/app", "app"},
		{"subdir", "/Users/me/Sites/app/src/Models", "/Users/me/Sites/app", "app/src/Models"},
		{"no project", "/Users/me/Sites/app", "", "app"},
		{"empty current", "", "/Users/me/Sites/app", ""},
		{"unrelated dirs", "/Users/me/other", "/Users/me/Sites/app", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := folderDisplay(tt.currentDir, tt.projectDir)
			if got != tt.want {
				t.Errorf("folderDisplay(%q, %q) = %q, want %q", tt.currentDir, tt.projectDir, got, tt.want)
			}
		})
	}
}

func TestEvalPromo(t *testing.T) {
	cfg := defaultPromo()

	// Before promo
	beforePromo := time.Unix(cfg.StartEpoch-3600, 0)
	display, color := evalPromo(cfg, beforePromo)
	if display != "" || color != "" {
		t.Errorf("Before promo: got %q/%q, want empty", display, color)
	}

	// After promo
	afterPromo := time.Unix(cfg.EndEpoch+3600, 0)
	display, color = evalPromo(cfg, afterPromo)
	if display != "" || color != "" {
		t.Errorf("After promo: got %q/%q, want empty", display, color)
	}

	// During promo — weekend (off-peak = 2x)
	// March 15, 2026 is a Sunday
	sunday := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	display, color = evalPromo(cfg, sunday)
	if display != "2x" {
		t.Errorf("Weekend promo: got %q, want 2x", display)
	}
	if color != ansiGreen {
		t.Errorf("Weekend promo color: got %q, want green", color)
	}
}

// ─── Phase 5: Output assembly tests ────────────────────────────────────────────

func TestAssembleOutput(t *testing.T) {
	input := statuslineInput{
		Model: statuslineModel{DisplayName: "Opus 4.6 (1M context)"},
		Workspace: statuslineWorkspace{
			CurrentDir: "/Users/me/Sites/claude",
			ProjectDir: "/Users/me/Sites/claude",
		},
		ContextWindow: statuslineContextWin{
			CurrentUsage:      statuslineUsage{60000, 0, 0},
			ContextWindowSize: 1000000,
		},
		Cost: statuslineCost{TotalDurationMs: 240000},
	}

	result := assembleOutput(input, "main", "+42/-3", "2x", ansiGreen)

	// Check key components are present
	checks := []string{
		"Opus 4.6 (1M context)", // model
		"6%",                    // percentage
		"4m",                    // duration
		"main",                  // branch
		"+42/-3",                // code stats
		"claude",                // folder
		"2x",                    // promo
	}

	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("Output missing %q:\n%s", check, result)
		}
	}
}

func TestAssembleOutputMinimal(t *testing.T) {
	input := statuslineInput{}
	result := assembleOutput(input, "", "", "", "")

	if !strings.Contains(result, "Claude") {
		t.Error("Minimal output should default to 'Claude' model name")
	}
	if !strings.Contains(result, "0%") {
		t.Error("Minimal output should show 0%")
	}
}

func TestAssembleOutput200KWarning(t *testing.T) {
	input := statuslineInput{
		Exceeds200K: true,
	}
	result := assembleOutput(input, "", "", "", "")
	if !strings.Contains(result, "200K+") {
		t.Error("Should contain 200K+ warning")
	}
}

func TestAssembleOutputWorktreeOverride(t *testing.T) {
	input := statuslineInput{
		Worktree: statuslineWorktree{Name: "my-worktree"},
		Workspace: statuslineWorkspace{
			CurrentDir: "/tmp/test",
		},
	}
	result := assembleOutput(input, "feat/some-branch", "", "", "")
	if !strings.Contains(result, "my-worktree") {
		t.Error("Should show worktree name instead of branch")
	}
	if strings.Contains(result, "feat/some-branch") {
		t.Error("Should NOT show branch when worktree name is set")
	}
}

func TestAssembleOutputNoGit(t *testing.T) {
	input := statuslineInput{
		Model: statuslineModel{DisplayName: "Claude"},
		Workspace: statuslineWorkspace{
			CurrentDir: "/tmp/test",
		},
	}
	result := assembleOutput(input, "", "", "", "")
	// Should not have double separators
	if strings.Contains(result, "│ │") {
		t.Error("Should not have double separators when git is absent")
	}
}

func TestAssembleOutputSeparatorOrder(t *testing.T) {
	input := statuslineInput{
		Model: statuslineModel{DisplayName: "Model"},
		Workspace: statuslineWorkspace{
			CurrentDir: "/tmp/test",
		},
		Cost: statuslineCost{TotalDurationMs: 240000},
	}
	result := assembleOutput(input, "main", "+10/-5", "2x", ansiGreen)

	// Code stats should NOT have a separator before it — it follows branch directly
	// The folder should ALWAYS have a separator
	mainIdx := strings.Index(result, "main")
	statsIdx := strings.Index(result, "+10/-5")
	folderIdx := strings.Index(result, "test")

	if mainIdx >= statsIdx {
		t.Error("Branch should come before code stats")
	}
	if statsIdx >= folderIdx {
		t.Error("Code stats should come before folder")
	}

	// Between code stats and folder, there should be a separator
	betweenStatsFolder := result[statsIdx:folderIdx]
	if !strings.Contains(betweenStatsFolder, "│") {
		t.Error("Should have separator between code stats and folder")
	}
}

// ─── Phase 3: Git info tests ────────────────────────────────────────────────────

// setupTestGitRepo creates a temp dir with git init, a tracked file, and one commit.
// Returns the directory path. The temp dir is cleaned up automatically by t.TempDir().
func setupTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	gitEnv := []string{
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	}

	// git init
	cmd := exec.Command("git", "init", dir)
	cmd.Env = append(os.Environ(), gitEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	// Set default branch to main
	cmd = exec.Command("git", "-C", dir, "checkout", "-b", "main")
	cmd.Env = append(os.Environ(), gitEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b main failed: %v\n%s", err, out)
	}

	// Create a tracked file
	testFile := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello world\n"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	// git add + commit
	cmd = exec.Command("git", "-C", dir, "add", "hello.txt")
	cmd.Env = append(os.Environ(), gitEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}

	cmd = exec.Command("git", "-C", dir, "commit", "-m", "initial commit")
	cmd.Env = append(os.Environ(), gitEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, out)
	}

	// Clean any cache file for this dir to ensure fresh results
	hash := fmt.Sprintf("%x", md5.Sum([]byte(dir)))
	cacheFile := filepath.Join(os.TempDir(), fmt.Sprintf("claude_statusline_git_%s", hash))
	os.Remove(cacheFile)
	os.Remove(cacheFile + ".tmp")

	return dir
}

func TestGitInfoCleanRepo(t *testing.T) {
	dir := setupTestGitRepo(t)

	gitInfo, codeStats := getGitInfo(dir)

	if gitInfo != "main" {
		t.Errorf("getGitInfo() gitInfo = %q, want %q", gitInfo, "main")
	}
	if codeStats != "" {
		t.Errorf("getGitInfo() codeStats = %q, want empty", codeStats)
	}
}

func TestGitInfoStagedChanges(t *testing.T) {
	dir := setupTestGitRepo(t)

	// Add new content to tracked file (unstaged)
	testFile := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello world\nnew line\nanother line\n"), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	gitInfo, codeStats := getGitInfo(dir)

	if gitInfo != "main" {
		t.Errorf("getGitInfo() gitInfo = %q, want %q", gitInfo, "main")
	}
	// Should show insertions (2 new lines) and deletions (0 — original line is still there)
	if !strings.Contains(codeStats, "+") {
		t.Errorf("getGitInfo() codeStats = %q, want to contain '+'", codeStats)
	}

	// Now stage the changes and verify staged diff is also counted
	gitEnv := []string{
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	}

	// Clear cache so we get fresh data
	hash := fmt.Sprintf("%x", md5.Sum([]byte(dir)))
	os.Remove(filepath.Join(os.TempDir(), fmt.Sprintf("claude_statusline_git_%s", hash)))

	cmd := exec.Command("git", "-C", dir, "add", "hello.txt")
	cmd.Env = append(os.Environ(), gitEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, out)
	}

	gitInfo2, codeStats2 := getGitInfo(dir)

	if gitInfo2 != "main" {
		t.Errorf("getGitInfo() gitInfo = %q, want %q", gitInfo2, "main")
	}
	// Staged changes should show in codeStats
	if !strings.Contains(codeStats2, "+") {
		t.Errorf("getGitInfo() codeStats (staged) = %q, want to contain '+'", codeStats2)
	}
}

func TestGitInfoOutsideRepo(t *testing.T) {
	// Use a temp dir that is NOT a git repo
	dir := t.TempDir()

	gitInfo, codeStats := getGitInfo(dir)

	if gitInfo != "" {
		t.Errorf("getGitInfo() outside repo: gitInfo = %q, want empty", gitInfo)
	}
	if codeStats != "" {
		t.Errorf("getGitInfo() outside repo: codeStats = %q, want empty", codeStats)
	}
}

func TestGitInfoEmptyDir(t *testing.T) {
	gitInfo, codeStats := getGitInfo("")

	if gitInfo != "" {
		t.Errorf("getGitInfo('') gitInfo = %q, want empty", gitInfo)
	}
	if codeStats != "" {
		t.Errorf("getGitInfo('') codeStats = %q, want empty", codeStats)
	}
}

func TestGitInfoCacheFreshness(t *testing.T) {
	dir := setupTestGitRepo(t)

	// First call — populates cache
	gitInfo1, codeStats1 := getGitInfo(dir)
	if gitInfo1 != "main" {
		t.Fatalf("First call: gitInfo = %q, want %q", gitInfo1, "main")
	}

	// Verify cache file exists
	hash := fmt.Sprintf("%x", md5.Sum([]byte(dir)))
	cacheFile := filepath.Join(os.TempDir(), fmt.Sprintf("claude_statusline_git_%s", hash))
	if _, err := os.Stat(cacheFile); os.IsNotExist(err) {
		t.Fatal("Cache file should exist after first getGitInfo call")
	}

	// Write known sentinel value to cache to verify it's read on next call
	sentinel := "cached-branch|+99/-88"
	if err := os.WriteFile(cacheFile, []byte(sentinel), 0644); err != nil {
		t.Fatalf("Failed to write sentinel to cache: %v", err)
	}

	// Second call within TTL — should read from cache (sentinel)
	gitInfo2, codeStats2 := getGitInfo(dir)
	if gitInfo2 != "cached-branch" {
		t.Errorf("Within TTL: gitInfo = %q, want %q (from cache)", gitInfo2, "cached-branch")
	}
	if codeStats2 != "+99/-88" {
		t.Errorf("Within TTL: codeStats = %q, want %q (from cache)", codeStats2, "+99/-88")
	}

	// Now backdate the cache file to expire it (older than 5 seconds)
	expired := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(cacheFile, expired, expired); err != nil {
		t.Fatalf("Failed to backdate cache file: %v", err)
	}

	// Third call — cache expired, should get fresh data
	gitInfo3, _ := getGitInfo(dir)
	if gitInfo3 != "main" {
		t.Errorf("After TTL expiry: gitInfo = %q, want %q (fresh)", gitInfo3, "main")
	}

	// Verify it's no longer the sentinel
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("Failed to read cache after refresh: %v", err)
	}
	if strings.Contains(string(data), "cached-branch") {
		t.Error("Cache should have been refreshed with real data after TTL expiry")
	}

	_ = codeStats1 // used in first assertion
}

func TestGitInfoCorruptCache(t *testing.T) {
	dir := setupTestGitRepo(t)

	// Write corrupt/malformed data to cache
	hash := fmt.Sprintf("%x", md5.Sum([]byte(dir)))
	cacheFile := filepath.Join(os.TempDir(), fmt.Sprintf("claude_statusline_git_%s", hash))

	// Write garbage with no pipe delimiter — should still parse without panic
	if err := os.WriteFile(cacheFile, []byte("garbage-no-pipe"), 0644); err != nil {
		t.Fatalf("Failed to write corrupt cache: %v", err)
	}

	// Should read from cache (malformed but parseable — single part, no pipe)
	gitInfo, codeStats := getGitInfo(dir)
	// With no pipe, parts[0] = "garbage-no-pipe", parts[1] doesn't exist
	// The code handles this: gi = trimmed parts[0], cs = ""
	if gitInfo != "garbage-no-pipe" {
		t.Errorf("Corrupt cache (no pipe): gitInfo = %q, want %q", gitInfo, "garbage-no-pipe")
	}
	if codeStats != "" {
		t.Errorf("Corrupt cache (no pipe): codeStats = %q, want empty", codeStats)
	}

	// Now write empty file — should also not panic
	os.Remove(cacheFile)
	if err := os.WriteFile(cacheFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write empty cache: %v", err)
	}

	gitInfo2, codeStats2 := getGitInfo(dir)
	// Empty file: parts[0] = "", trimmed = ""
	if gitInfo2 != "" {
		t.Errorf("Empty cache: gitInfo = %q, want empty", gitInfo2)
	}
	if codeStats2 != "" {
		t.Errorf("Empty cache: codeStats = %q, want empty", codeStats2)
	}

	// Backdate to expire, verify fresh data is returned
	expired := time.Now().Add(-10 * time.Second)
	os.Chtimes(cacheFile, expired, expired)

	gitInfo3, _ := getGitInfo(dir)
	if gitInfo3 != "main" {
		t.Errorf("After corrupt cache expiry: gitInfo = %q, want %q", gitInfo3, "main")
	}
}

func TestGitInfoDetachedHead(t *testing.T) {
	dir := setupTestGitRepo(t)

	gitEnv := []string{
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	}

	// Get the current commit hash
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	cmd.Env = append(os.Environ(), gitEnv...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed: %v", err)
	}
	commitHash := strings.TrimSpace(string(out))

	// Detach HEAD
	cmd = exec.Command("git", "-C", dir, "checkout", commitHash)
	cmd.Env = append(os.Environ(), gitEnv...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout detached failed: %v\n%s", err, out)
	}

	// Clear cache
	hash := fmt.Sprintf("%x", md5.Sum([]byte(dir)))
	os.Remove(filepath.Join(os.TempDir(), fmt.Sprintf("claude_statusline_git_%s", hash)))

	gitInfo, codeStats := getGitInfo(dir)

	// In detached HEAD, `git branch --show-current` returns empty string
	// The function returns ("", "") when branch is empty
	if gitInfo != "" {
		t.Errorf("Detached HEAD: gitInfo = %q, want empty", gitInfo)
	}
	if codeStats != "" {
		t.Errorf("Detached HEAD: codeStats = %q, want empty", codeStats)
	}
}
