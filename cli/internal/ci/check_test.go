package ci

import (
	"fmt"
	"strings"
	"testing"
)

// mockRunner builds a runCommand replacement from a map of command→output pairs.
func mockRunner(responses map[string]mockResp) func(string, ...string) (string, error) {
	return func(name string, args ...string) (string, error) {
		key := name + " " + strings.Join(args, " ")
		for pattern, resp := range responses {
			if strings.Contains(key, pattern) {
				return resp.out, resp.err
			}
		}
		return "", fmt.Errorf("unmocked command: %s", key)
	}
}

type mockResp struct {
	out string
	err error
}

func TestCheck_NoGHRepo(t *testing.T) {
	original := runCommand
	defer func() { runCommand = original }()

	runCommand = mockRunner(map[string]mockResp{
		"repo view": {out: "", err: fmt.Errorf("not a gh repo")},
	})

	result, err := Check("tests.yml", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasCI {
		t.Error("expected HasCI=false when not in a gh repo")
	}
	if result.Relevant {
		t.Error("expected Relevant=false")
	}
}

func TestCheck_NoWorkflowFile(t *testing.T) {
	original := runCommand
	defer func() { runCommand = original }()

	runCommand = mockRunner(map[string]mockResp{
		"repo view": {out: "owner/repo", err: nil},
		"api":       {out: "", err: fmt.Errorf("404 not found")},
	})

	result, err := Check("tests.yml", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasCI {
		t.Error("expected HasCI=false when workflow file doesn't exist")
	}
}

func TestCheck_HasFileButNoRuns(t *testing.T) {
	original := runCommand
	defer func() { runCommand = original }()

	runCommand = mockRunner(map[string]mockResp{
		"repo view": {out: "owner/repo", err: nil},
		"api":       {out: "tests.yml", err: nil},
		"run list":  {out: "[]", err: nil},
	})

	result, err := Check("tests.yml", "feat/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasCI {
		t.Error("expected HasCI=true when workflow file exists")
	}
	if result.Relevant {
		t.Error("expected Relevant=false when no runs exist")
	}
	if result.LastRun != "" {
		t.Errorf("expected empty LastRun, got %q", result.LastRun)
	}
}

func TestCheck_HasRunsButNotOnBranch(t *testing.T) {
	original := runCommand
	defer func() { runCommand = original }()

	runsJSON := `[{"databaseId":1,"createdAt":"2026-03-30T10:00:00Z","status":"completed"}]`

	callCount := 0
	runCommand = func(name string, args ...string) (string, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.Contains(key, "repo view") {
			return "owner/repo", nil
		}
		if strings.Contains(key, "api") {
			return "tests.yml", nil
		}
		if strings.Contains(key, "run list") {
			callCount++
			if callCount == 1 {
				// Tier 2: recent runs
				return runsJSON, nil
			}
			// Tier 3: branch runs — empty
			return "[]", nil
		}
		return "", fmt.Errorf("unmocked: %s", key)
	}

	result, err := Check("tests.yml", "feat/unrelated")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasCI {
		t.Error("expected HasCI=true")
	}
	if result.LastRun != "2026-03-30T10:00:00Z" {
		t.Errorf("expected LastRun=2026-03-30T10:00:00Z, got %q", result.LastRun)
	}
	if result.Relevant {
		t.Error("expected Relevant=false when no runs on branch")
	}
}

func TestCheck_FullyRelevant(t *testing.T) {
	original := runCommand
	defer func() { runCommand = original }()

	runsJSON := `[{"databaseId":1,"createdAt":"2026-03-31T08:00:00Z","status":"completed"}]`
	branchRunsJSON := `[{"databaseId":1}]`

	callCount := 0
	runCommand = func(name string, args ...string) (string, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.Contains(key, "repo view") {
			return "owner/repo", nil
		}
		if strings.Contains(key, "api") {
			return "tests.yml", nil
		}
		if strings.Contains(key, "run list") {
			callCount++
			if callCount == 1 {
				return runsJSON, nil
			}
			return branchRunsJSON, nil
		}
		return "", fmt.Errorf("unmocked: %s", key)
	}

	result, err := Check("tests.yml", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasCI {
		t.Error("expected HasCI=true")
	}
	if result.LastRun != "2026-03-31T08:00:00Z" {
		t.Errorf("expected LastRun=2026-03-31T08:00:00Z, got %q", result.LastRun)
	}
	if !result.Relevant {
		t.Error("expected Relevant=true when runs exist on branch")
	}
}

func TestCICheckResult_JSON(t *testing.T) {
	result := &CICheckResult{
		HasCI:    true,
		Workflow: "tests.yml",
		LastRun:  "2026-03-31T08:00:00Z",
		Relevant: true,
	}
	j := result.JSON()
	if !strings.Contains(j, `"has_ci": true`) {
		t.Errorf("JSON output missing has_ci: %s", j)
	}
	if !strings.Contains(j, `"relevant": true`) {
		t.Errorf("JSON output missing relevant: %s", j)
	}
}
