package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bravros/bravros/cli/internal/doctor"
)

// TestDoctorCmd_Registered verifies the doctor verb is wired onto rootCmd with
// its full flag set — the SessionStart hook depends on `--quick` and `--json`.
func TestDoctorCmd_Registered(t *testing.T) {
	var registered bool
	for _, c := range rootCmd.Commands() {
		if c.Name() == "doctor" {
			registered = true
			for _, f := range []string{"quick", "deep", "install-missing", "fix", "json"} {
				if c.Flags().Lookup(f) == nil {
					t.Errorf("doctor: missing --%s flag", f)
				}
			}
		}
	}
	if !registered {
		t.Fatal("doctor command is not registered on rootCmd")
	}
}

// TestDoctorRun_QuickMode exercises the engine wiring: quick mode must produce a
// report with checks and a non-empty aggregate status.
func TestDoctorRun_QuickMode(t *testing.T) {
	var report *doctor.DoctorReport
	out := captureStdout(t, func() {
		var err error
		report, err = runDoctor(doctor.RunOpts{Quick: true})
		if err != nil {
			t.Fatalf("runDoctor: %v", err)
		}
	})

	if report == nil || len(report.Checks) == 0 {
		t.Fatal("expected at least one check in the quick-mode report")
	}
	if report.Status == "" {
		t.Fatal("expected a non-empty aggregate status")
	}
	if !strings.Contains(out, "Status:") {
		t.Fatalf("expected human output to end with a Status line, got %q", out)
	}
}

// TestDoctorRun_JSONContract checks the SessionStart-hook contract: healthy runs
// print nothing; unhealthy runs print a single parseable JSON object.
func TestDoctorRun_JSONContract(t *testing.T) {
	var report *doctor.DoctorReport
	out := captureStdout(t, func() {
		var err error
		report, err = runDoctor(doctor.RunOpts{Quick: true, JSONOutput: true})
		if err != nil {
			t.Fatalf("runDoctor: %v", err)
		}
	})

	if report.Status == "healthy" {
		if strings.TrimSpace(out) != "" {
			t.Fatalf("healthy JSON run must be silent, got %q", out)
		}
		return
	}

	var decoded doctor.DoctorReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &decoded); err != nil {
		t.Fatalf("unhealthy JSON run must emit parseable JSON, got %q (%v)", out, err)
	}
	if decoded.Status != report.Status {
		t.Fatalf("JSON status %q != report status %q", decoded.Status, report.Status)
	}
}
