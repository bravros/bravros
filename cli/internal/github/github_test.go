package github

import (
	"strings"
	"testing"
)

// --- Pure function tests ---

func TestSection(t *testing.T) {
	t.Run("short title", func(t *testing.T) {
		out := Section("Reviews")
		if !strings.HasPrefix(out, "\n── Reviews ") {
			t.Fatalf("unexpected prefix: %q", out)
		}
		// Title "Reviews" is 7 chars, padding = 54-7 = 47 dashes
		dashes := strings.Count(out, "─")
		// The prefix has 2 dashes ("──") plus the padding dashes
		expectedPadding := 54 - len("Reviews")
		if dashes != 2+expectedPadding {
			t.Fatalf("expected %d total dash runes, got %d", 2+expectedPadding, dashes)
		}
	})

	t.Run("long title exceeding padding", func(t *testing.T) {
		long := strings.Repeat("X", 60)
		out := Section(long)
		if !strings.Contains(out, long) {
			t.Fatal("section should contain the full title")
		}
		// padding should be 0 — only the 2 prefix dashes
		suffix := out[strings.Index(out, long)+len(long):]
		suffix = strings.TrimSpace(suffix)
		if suffix != "" {
			t.Fatalf("expected no trailing dashes for oversized title, got %q", suffix)
		}
	})

	t.Run("empty title", func(t *testing.T) {
		out := Section("")
		if !strings.HasPrefix(out, "\n──  ") {
			t.Fatalf("unexpected output for empty title: %q", out)
		}
	})
}

func TestGetPRNumber_withExplicitArg(t *testing.T) {
	num, err := GetPRNumber("42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != "42" {
		t.Fatalf("expected '42', got %q", num)
	}
}

func TestRun_simple(t *testing.T) {
	out, _, err := Run("echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Fatalf("expected 'hello', got %q", out)
	}
}

func TestRun_stderrCapture(t *testing.T) {
	_, stderr, err := Run("sh", "-c", "echo oops >&2 && exit 1")
	if err == nil {
		t.Fatal("expected error for failing command")
	}
	if stderr != "oops" {
		t.Fatalf("expected stderr 'oops', got %q", stderr)
	}
}

// --- Integration tests against real repo bravros/private ---

func TestGetRepo(t *testing.T) {
	repo, err := GetRepo()
	if err != nil {
		t.Skipf("skipping: gh not available or not in a repo: %v", err)
	}
	if repo != "bravros/private" {
		t.Fatalf("expected 'bravros/private', got %q", repo)
	}
}

func TestFetchPRInfo(t *testing.T) {
	info, err := FetchPRInfo("16")
	if err != nil {
		t.Skipf("skipping: gh not available or PR #16 not found: %v", err)
	}
	if info["title"] == nil || info["title"] == "" {
		t.Fatal("expected non-empty title")
	}
	if info["state"] == nil {
		t.Fatal("expected state field")
	}
	if info["url"] == nil {
		t.Fatal("expected url field")
	}
}

func TestFetchChangedFiles(t *testing.T) {
	files, err := FetchChangedFiles("16")
	if err != nil {
		t.Skipf("skipping: gh not available or PR #16 not found: %v", err)
	}
	if files == "" {
		t.Fatal("expected at least one changed file")
	}
	lines := strings.Split(files, "\n")
	if len(lines) == 0 {
		t.Fatal("expected file list to have lines")
	}
}

func TestFetchDiff(t *testing.T) {
	diff, err := FetchDiff("16")
	if err != nil {
		t.Skipf("skipping: gh not available or PR #16 not found: %v", err)
	}
	if !strings.Contains(diff, "diff") {
		t.Fatal("expected diff output to contain 'diff'")
	}
}

func TestFetchReviews(t *testing.T) {
	// This may return empty if PR #16 has no reviews — that's OK
	_, err := FetchReviews("16")
	if err != nil {
		t.Skipf("skipping: gh not available or PR #16 not found: %v", err)
	}
}

func TestFetchInlineComments(t *testing.T) {
	// May return empty — just verify no hard error
	_, err := FetchInlineComments("bravros/private", "16")
	if err != nil {
		t.Skipf("skipping: gh not available: %v", err)
	}
}

func TestFetchPRChecks(t *testing.T) {
	out, err := FetchPRChecks("16")
	if err != nil {
		t.Skipf("skipping: gh not available or PR #16 not found: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty checks output")
	}
}

func TestFetchBotComments(t *testing.T) {
	// May return empty — just verify no hard error
	_, err := FetchBotComments("bravros/private", "16", "github-actions")
	if err != nil {
		t.Skipf("skipping: gh not available: %v", err)
	}
}

func TestFetchLatestBotComment(t *testing.T) {
	_, err := FetchLatestBotComment("bravros/private", "16", "github-actions")
	if err != nil {
		t.Skipf("skipping: gh not available: %v", err)
	}
}

func TestFetchHumanComments(t *testing.T) {
	_, err := FetchHumanComments("bravros/private", "16", "github-actions")
	if err != nil {
		t.Skipf("skipping: gh not available: %v", err)
	}
}
