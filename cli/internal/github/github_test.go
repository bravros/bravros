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

// --- Integration tests that shell out to a real `gh` against the current repo ---

// TestGetRepo asserts the SHAPE of GetRepo's result, not a specific repository.
// GetRepo returns `gh repo view --json nameWithOwner` for whatever checkout the
// test runs in, so pinning it to a literal slug makes the test a property of the
// developer's working directory rather than of the function. The previous version
// accepted only "sbravros/claude" or "bravros/bravros" and therefore failed on
// every checkout of bravros/private — a permanent red that said nothing about
// GetRepo. The contract worth testing is: a non-empty "owner/name" pair.
func TestGetRepo(t *testing.T) {
	repo, err := GetRepo()
	if err != nil {
		t.Skipf("skipping: gh not available or not in a repo: %v", err)
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		t.Fatalf("GetRepo() = %q, want a non-empty \"owner/name\" pair", repo)
	}
	if strings.ContainsAny(repo, " \t\n") {
		t.Errorf("GetRepo() = %q, want no surrounding or embedded whitespace", repo)
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
	_, err := FetchInlineComments("sbravros/claude", "16")
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
	_, err := FetchBotComments("sbravros/claude", "16", "github-actions")
	if err != nil {
		t.Skipf("skipping: gh not available: %v", err)
	}
}

func TestFetchLatestBotComment(t *testing.T) {
	_, err := FetchLatestBotComment("sbravros/claude", "16", "github-actions")
	if err != nil {
		t.Skipf("skipping: gh not available: %v", err)
	}
}

func TestFetchHumanComments(t *testing.T) {
	_, err := FetchHumanComments("sbravros/claude", "16", "github-actions")
	if err != nil {
		t.Skipf("skipping: gh not available: %v", err)
	}
}
