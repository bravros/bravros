package cmd

import (
	"crypto/md5"
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

// ─── ANSI color constants ──────────────────────────────────────────────────────

const (
	ansiReset   = "\033[0m"
	ansiDim     = "\033[2m"
	ansiBold    = "\033[1m"
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
)

// ─── JSON input structs ────────────────────────────────────────────────────────

type statuslineInput struct {
	Model         statuslineModel      `json:"model"`
	Workspace     statuslineWorkspace  `json:"workspace"`
	ContextWindow statuslineContextWin `json:"context_window"`
	Cost          statuslineCost       `json:"cost"`
	Exceeds200K   bool                 `json:"exceeds_200k_tokens"`
	Worktree      statuslineWorktree   `json:"worktree"`
}

type statuslineModel struct {
	DisplayName string `json:"display_name"`
}

type statuslineWorkspace struct {
	CurrentDir string `json:"current_dir"`
	ProjectDir string `json:"project_dir"`
}

type statuslineContextWin struct {
	CurrentUsage      statuslineUsage `json:"current_usage"`
	ContextWindowSize int64           `json:"context_window_size"`
}

type statuslineUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type statuslineCost struct {
	TotalDurationMs int64 `json:"total_duration_ms"`
}

type statuslineWorktree struct {
	Name string `json:"name"`
}

// ─── Promo configuration ───────────────────────────────────────────────────────

type promoConfig struct {
	StartEpoch int64 // Unix timestamp for promo start
	EndEpoch   int64 // Unix timestamp for promo end
	PeakStart  int   // Hour (0-23) in ET when peak starts
	PeakEnd    int   // Hour (0-23) in ET when peak ends (exclusive)
	PeakDays   []int // ISO weekday numbers (1=Mon, 7=Sun) that count as peak
}

// defaultPromo returns the March 2026 promotion config
func defaultPromo() promoConfig {
	return promoConfig{
		StartEpoch: 1773370800, // 2026-03-13 00:00:00 local
		EndEpoch:   1774753199, // 2026-03-28 23:59:59 local
		PeakStart:  8,
		PeakEnd:    14,
		PeakDays:   []int{1, 2, 3, 4, 5}, // Mon-Fri
	}
}

// ─── Git cache ─────────────────────────────────────────────────────────────────

type gitCacheEntry struct {
	GitInfo   string
	CodeStats string
}

// ─── Pure computation functions (testable) ─────────────────────────────────────

// calcTokenUsage returns total token count from all input sources
func calcTokenUsage(u statuslineUsage) int64 {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// calcPercentage returns usage percentage (0-100+)
func calcPercentage(current, size int64) int {
	if size <= 0 {
		return 0
	}
	return int(current * 100 / size)
}

// buildProgressBar returns a 10-char progress bar string
func buildProgressBar(pct int) string {
	barWidth := 10
	filled := pct * barWidth / 100
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}
	empty := barWidth - filled
	return strings.Repeat("█", filled) + strings.Repeat("░", empty)
}

// barColor returns the ANSI color for the progress bar based on percentage
func barColor(pct int) string {
	if pct <= 50 {
		return ansiGreen
	}
	if pct <= 65 {
		return ansiYellow
	}
	return ansiRed
}

// formatDuration converts milliseconds to "Xh Ym" or "Xm" format
func formatDuration(ms int64) string {
	if ms <= 0 {
		return ""
	}
	totalSec := ms / 1000
	hours := totalSec / 3600
	mins := (totalSec % 3600) / 60
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, mins)
	}
	if mins > 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return ""
}

// calcRemainingTime estimates remaining session time based on burn rate
func calcRemainingTime(current, size, durationMs int64) string {
	if durationMs <= 0 || current <= 0 || size <= 0 {
		return ""
	}
	totalSec := durationMs / 1000
	if totalSec <= 60 {
		return ""
	}
	remaining := size - current
	if remaining <= 0 {
		return ""
	}
	estSec := remaining * totalSec / current
	estHours := estSec / 3600
	estMins := (estSec % 3600) / 60
	if estHours > 0 {
		return fmt.Sprintf("~%dh%dm left", estHours, estMins)
	}
	if estMins > 0 {
		return fmt.Sprintf("~%dm left", estMins)
	}
	return ""
}

// folderDisplay returns the display name for the current directory
func folderDisplay(currentDir, projectDir string) string {
	if currentDir == "" {
		return ""
	}
	if projectDir != "" && currentDir != projectDir {
		relPath := strings.TrimPrefix(currentDir, projectDir+"/")
		if relPath != currentDir {
			return filepath.Base(projectDir) + "/" + relPath
		}
	}
	return filepath.Base(currentDir)
}

// evalPromo evaluates the promotion display at the given time
func evalPromo(cfg promoConfig, now time.Time) (display string, color string) {
	epoch := now.Unix()
	if epoch < cfg.StartEpoch || epoch > cfg.EndEpoch {
		return "", ""
	}

	// Convert to ET
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		// Fallback: treat as off-peak
		return "2x", ansiGreen
	}
	et := now.In(loc)
	hour := et.Hour()
	dow := int(et.Weekday())
	// Convert Go weekday (0=Sun) to ISO (1=Mon, 7=Sun)
	isoDow := dow
	if isoDow == 0 {
		isoDow = 7
	}

	isPeakDay := false
	for _, d := range cfg.PeakDays {
		if d == isoDow {
			isPeakDay = true
			break
		}
	}

	if isPeakDay && hour >= cfg.PeakStart && hour < cfg.PeakEnd {
		return "1x", ansiYellow
	}
	return "2x", ansiGreen
}

// ─── Git helpers ───────────────────────────────────────────────────────────────

// getGitInfo retrieves branch, ahead/behind, and diff stats with caching
func getGitInfo(currentDir string) (gitInfo string, codeStats string) {
	if currentDir == "" {
		return "", ""
	}

	hash := fmt.Sprintf("%x", md5.Sum([]byte(currentDir)))
	cacheFile := filepath.Join(os.TempDir(), fmt.Sprintf("claude_statusline_git_%s", hash))

	// Check cache freshness
	if info, err := os.Stat(cacheFile); err == nil {
		age := time.Since(info.ModTime()).Seconds()
		if age <= 5 {
			data, err := os.ReadFile(cacheFile)
			if err == nil {
				parts := strings.SplitN(string(data), "|", 2)
				gi := ""
				cs := ""
				if len(parts) >= 1 {
					gi = strings.TrimSpace(parts[0])
				}
				if len(parts) >= 2 {
					cs = strings.TrimSpace(parts[1])
				}
				return gi, cs
			}
		}
	}

	// Check if it's a git repo
	checkCmd := exec.Command("git", "-C", currentDir, "rev-parse", "--git-dir")
	checkCmd.Stderr = nil
	if err := checkCmd.Run(); err != nil {
		return "", ""
	}

	// Get current branch
	branchCmd := exec.Command("git", "-C", currentDir, "--no-optional-locks", "branch", "--show-current")
	branchCmd.Stderr = nil
	branchOut, err := branchCmd.Output()
	if err != nil {
		return "", ""
	}
	branch := strings.TrimSpace(string(branchOut))
	if branch == "" {
		return "", ""
	}

	// Get diff stats (working + staged)
	ins, del := 0, 0
	for _, diffArgs := range [][]string{
		{"git", "-C", currentDir, "--no-optional-locks", "diff", "--numstat"},
		{"git", "-C", currentDir, "--no-optional-locks", "diff", "--cached", "--numstat"},
	} {
		cmd := exec.Command(diffArgs[0], diffArgs[1:]...)
		cmd.Stderr = nil
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			if fields[0] != "-" {
				if v, err := strconv.Atoi(fields[0]); err == nil {
					ins += v
				}
			}
			if fields[1] != "-" {
				if v, err := strconv.Atoi(fields[1]); err == nil {
					del += v
				}
			}
		}
	}

	if ins > 0 || del > 0 {
		codeStats = fmt.Sprintf("+%d/-%d", ins, del)
	}

	// Ahead/behind
	upstreamInfo := ""
	upstreamCmd := exec.Command("git", "-C", currentDir, "--no-optional-locks", "rev-parse", "--abbrev-ref", "@{upstream}")
	upstreamCmd.Stderr = nil
	if upstreamOut, err := upstreamCmd.Output(); err == nil && strings.TrimSpace(string(upstreamOut)) != "" {
		aheadCmd := exec.Command("git", "-C", currentDir, "--no-optional-locks", "rev-list", "--count", "@{upstream}..HEAD")
		aheadCmd.Stderr = nil
		if aheadOut, err := aheadCmd.Output(); err == nil {
			if ahead, err := strconv.Atoi(strings.TrimSpace(string(aheadOut))); err == nil && ahead > 0 {
				upstreamInfo = fmt.Sprintf("↑%d", ahead)
			}
		}
		behindCmd := exec.Command("git", "-C", currentDir, "--no-optional-locks", "rev-list", "--count", "HEAD..@{upstream}")
		behindCmd.Stderr = nil
		if behindOut, err := behindCmd.Output(); err == nil {
			if behind, err := strconv.Atoi(strings.TrimSpace(string(behindOut))); err == nil && behind > 0 {
				upstreamInfo += fmt.Sprintf("↓%d", behind)
			}
		}
		if upstreamInfo != "" {
			upstreamInfo = " " + upstreamInfo
		}
	}

	gitInfo = branch + upstreamInfo

	// Write cache atomically (write to temp, then rename)
	tmpFile := cacheFile + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(gitInfo+"|"+codeStats), 0644); err == nil {
		// Rename is atomic on same filesystem; if it fails, just continue with fresh data
		_ = os.Rename(tmpFile, cacheFile)
	}

	return gitInfo, codeStats
}

// ─── Output assembly ───────────────────────────────────────────────────────────

// assembleOutput builds the final ANSI-colored status line
func assembleOutput(input statuslineInput, gitInfo, codeStats, promoDisplay, promoColor string) string {
	// Model name
	modelName := input.Model.DisplayName
	if modelName == "" {
		modelName = "Claude"
	}

	// Context calculation
	current := calcTokenUsage(input.ContextWindow.CurrentUsage)
	size := input.ContextWindow.ContextWindowSize
	if size == 0 {
		size = 1
	}
	pct := calcPercentage(current, size)
	bar := buildProgressBar(pct)
	bColor := barColor(pct)

	// Duration
	durationDisplay := formatDuration(input.Cost.TotalDurationMs)

	// Remaining time
	remainingDisplay := calcRemainingTime(current, input.ContextWindow.ContextWindowSize, input.Cost.TotalDurationMs)

	// Folder
	dirDisplay := folderDisplay(input.Workspace.CurrentDir, input.Workspace.ProjectDir)

	// Worktree override for branch display
	branchDisplay := gitInfo
	if input.Worktree.Name != "" && gitInfo != "" {
		branchDisplay = input.Worktree.Name
	}

	// Build output
	var out strings.Builder

	// Model
	out.WriteString(ansiMagenta + modelName + ansiReset)

	// Context bar + percentage
	out.WriteString(" " + ansiDim + "│" + ansiReset + " ")
	out.WriteString(bColor + bar + ansiReset + " " + ansiCyan + fmt.Sprintf("%d%%", pct) + ansiReset)

	// 200K+ warning
	if input.Exceeds200K {
		out.WriteString(" " + ansiRed + ansiBold + "200K+" + ansiReset)
	}

	// Remaining time
	if remainingDisplay != "" {
		out.WriteString(" " + ansiDim + remainingDisplay + ansiReset)
	}

	// Promo
	if promoDisplay != "" {
		out.WriteString(" " + ansiDim + "│" + ansiReset + " " + promoColor + ansiBold + promoDisplay + ansiReset)
	}

	// Duration
	if durationDisplay != "" {
		out.WriteString(" " + ansiDim + "│" + ansiReset + " " + ansiYellow + durationDisplay + ansiReset)
	}

	// Git branch
	if branchDisplay != "" {
		out.WriteString(" " + ansiDim + "│" + ansiReset + " " + ansiGreen + branchDisplay + ansiReset)
	}

	// Code stats (no separator — directly after branch with just a space)
	if codeStats != "" {
		out.WriteString(" " + ansiYellow + codeStats + ansiReset)
	}

	// Folder (always has separator)
	if dirDisplay != "" {
		out.WriteString(" " + ansiDim + "│" + ansiReset + " " + ansiBlue + dirDisplay + ansiReset)
	}

	return out.String()
}

// ─── Cobra command ─────────────────────────────────────────────────────────────

var statuslineCmd = &cobra.Command{
	Use:   "statusline",
	Short: "Render Claude Code status line (reads JSON from stdin)",
	Long:  "Replaces statusline.sh — reads Claude Code session JSON from stdin and outputs an ANSI-colored status line.",
	Run: func(cmd *cobra.Command, args []string) {
		runStatusline(os.Stdin)
	},
}

func runStatusline(r io.Reader) {
	data, err := io.ReadAll(r)
	if err != nil {
		fmt.Println("Claude")
		return
	}

	var input statuslineInput
	if err := json.Unmarshal(data, &input); err != nil {
		fmt.Println("Claude")
		return
	}

	// Git info
	gitInfo, codeStats := getGitInfo(input.Workspace.CurrentDir)

	// Promo
	promo := defaultPromo()
	promoDisplay, promoColor := evalPromo(promo, time.Now())

	// Assemble and print
	output := assembleOutput(input, gitInfo, codeStats, promoDisplay, promoColor)
	fmt.Println(output)
}
