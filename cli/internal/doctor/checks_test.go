package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRun_QuickMode_Healthy(t *testing.T) {
	// Quick mode should not panic and should return a valid report.
	report, err := Run(RunOpts{Quick: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.Status == "" {
		t.Error("expected non-empty status")
	}
	if len(report.Checks) == 0 {
		t.Error("expected at least one check")
	}
}

func TestRun_DeepMode(t *testing.T) {
	report, err := Run(RunOpts{Deep: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Deep mode should have more checks than quick mode.
	qReport, _ := Run(RunOpts{Quick: true})
	if len(report.Checks) <= len(qReport.Checks) {
		t.Errorf("deep mode should produce more checks than quick mode; got %d vs %d",
			len(report.Checks), len(qReport.Checks))
	}
}

func TestCheckBinary_FoundBinary(t *testing.T) {
	// "git" must be present in any dev environment.
	results := checkBinary("git", "git VCS")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != SeverityOK {
		t.Errorf("expected ok for git, got %s: %s", results[0].Status, results[0].Message)
	}
}

func TestCheckBinary_MissingBinary(t *testing.T) {
	// Use a fake binary name that will never exist.
	results := checkBinary("kaisser-nonexistent-tool-xyz", "fake tool")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status == SeverityOK {
		t.Error("expected non-ok status for missing binary")
	}
}

func TestCheckBinary_SafeListRestriction(t *testing.T) {
	// "op" is NOT in the safe list.
	if isSafeToInstall("op") {
		t.Error("op should NOT be in the safe install list")
	}
	// "claude" is NOT in the safe list.
	if isSafeToInstall("claude") {
		t.Error("claude should NOT be in the safe install list")
	}
	// "gh" IS in the safe list.
	if !isSafeToInstall("gh") {
		t.Error("gh should be in the safe install list")
	}
}

func TestCheckClaudeConfigDir_Missing(t *testing.T) {
	// Override HOME to a temp dir so ~/.claude doesn't exist.
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	results := checkClaudeConfigDir()
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Status == SeverityOK {
		t.Error("expected non-ok when ~/.claude missing")
	}
}

func TestCheckClaudeConfigDir_Present(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".claude"), 0755)

	results := checkClaudeConfigDir()
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Status != SeverityOK {
		t.Errorf("expected ok when ~/.claude present, got %s", results[0].Status)
	}
}

func TestAggregateStatus(t *testing.T) {
	tests := []struct {
		checks []CheckResult
		want   string
	}{
		{[]CheckResult{{Status: SeverityOK}}, "healthy"},
		{[]CheckResult{{Status: SeverityWarn}}, "degraded"},
		{[]CheckResult{{Status: SeverityError}}, "critical"},
		{[]CheckResult{{Status: SeverityCritical}}, "critical"},
		{[]CheckResult{{Status: SeverityOK}, {Status: SeverityWarn}}, "degraded"},
	}
	for _, tc := range tests {
		got := aggregateStatus(tc.checks)
		if got != tc.want {
			t.Errorf("aggregateStatus(%v) = %q, want %q", tc.checks, got, tc.want)
		}
	}
}

func TestInstallMissing_ProposesPackages(t *testing.T) {
	report, err := Run(RunOpts{Quick: true, InstallMissing: true})
	if err != nil {
		t.Fatal(err)
	}
	// InstallsProposed should be a slice (may be empty on fully-equipped dev machine).
	if report.InstallsProposed == nil && report.Status != "healthy" {
		t.Error("expected InstallsProposed to be non-nil when status is not healthy")
	}
}

// Run with Fix=true must rewrite the managed sections in settings.json +
// CLAUDE.md (mirrors `kaisser config sync`) and report what was repaired.
func TestRun_FixMode_AppliesRepairs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}

	report, err := Run(RunOpts{Quick: true, Fix: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.RepairsApplied) == 0 {
		t.Fatal("expected RepairsApplied to be non-empty when Fix=true")
	}
	settings := filepath.Join(dir, ".claude", "settings.json")
	if _, err := os.Stat(settings); err != nil {
		t.Errorf("expected settings.json to be written: %v", err)
	}
	claudeMD := filepath.Join(dir, ".claude", "CLAUDE.md")
	if _, err := os.Stat(claudeMD); err != nil {
		t.Errorf("expected CLAUDE.md to be written: %v", err)
	}
}

// proposeInstalls must yield raw binary names regardless of OS-specific
// FixHint formatting. Previously it parsed the last word of FixHint, which on
// Linux returned "equivalent)" instead of the package name.
func TestProposeInstalls_UsesFixBinaryNotHint(t *testing.T) {
	checks := []CheckResult{
		// Linux-style hint with parenthetical suffix — must NOT be parsed.
		{
			Name:      "GitHub CLI",
			Status:    SeverityWarn,
			CanFix:    true,
			FixHint:   "apt-get install -y gh  (or equivalent)",
			FixBinary: "gh",
		},
		// macOS-style hint.
		{
			Name:      "jq JSON processor",
			Status:    SeverityWarn,
			CanFix:    true,
			FixHint:   "brew install jq",
			FixBinary: "jq",
		},
		// OK status — must be skipped.
		{Name: "git", Status: SeverityOK, FixBinary: "git"},
		// CanFix=false — must be skipped.
		{Name: "claude", Status: SeverityWarn, CanFix: false, FixBinary: "claude"},
	}
	got := proposeInstalls(checks)
	want := []string{"gh", "jq"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}
