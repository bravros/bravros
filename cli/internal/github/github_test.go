package github

import (
	"fmt"
	"os/exec"
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

// TestBotLoginPredicate_WidensBareActionLogin verifies the jq predicate
// accepts the bare @claude Action login ("claude") when botLogin is the
// default "claude[bot]" — mirroring cmd.isBotOrAction's widening. Without
// this, FetchLatestBotCommentJSON's own jq filter would silently exclude
// every Action-posted comment before Go ever sees it (B-0023).
func TestBotLoginPredicate_WidensBareActionLogin(t *testing.T) {
	tests := []struct {
		name     string
		botLogin string
		login    string
		want     bool
	}{
		{"bare claude matches claude[bot] default", "claude[bot]", "claude", true},
		{"exact claude[bot] match", "claude[bot]", "claude[bot]", true},
		{"other [bot] suffix matches generic clause", "claude[bot]", "dependabot[bot]", true},
		{"human login does not match", "claude[bot]", "alice", false},
		{"bare login for a non-[bot] botLogin is NOT widened", "github-actions", "github", false},
		{"exact non-bot botLogin still matches itself", "github-actions", "github-actions", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pred := botLoginPredicate(tc.botLogin)
			doc := fmt.Sprintf(`[{"user":{"login":%q}}]`, tc.login)
			out, err := runJQBool(t, doc, pred)
			if err != nil {
				t.Fatalf("jq failed: %v", err)
			}
			if out != tc.want {
				t.Errorf("botLoginPredicate(%q) over login %q = %v; want %v", tc.botLogin, tc.login, out, tc.want)
			}
		})
	}
}

// runJQBool evaluates `.[] | select(<pred>)` against a one-element JSON array
// document and reports whether the element survived the select — i.e.
// whether pred evaluated truthy for it. Requires a `jq` binary on PATH;
// skips (not fails) when unavailable so this stays a CI-portable unit test.
func runJQBool(t *testing.T, jsonArrayDoc, pred string) (bool, error) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available on PATH")
	}
	cmd := exec.Command("jq", "-e", fmt.Sprintf(".[] | select(%s)", pred))
	cmd.Stdin = strings.NewReader(jsonArrayDoc)
	out, err := cmd.Output()
	if err != nil {
		// jq -e exits 1 when the last output is false/null, and 4 when
		// select() produced no output at all (both mean "predicate false"
		// here, since our document is a single-element array).
		if exitErr, ok := err.(*exec.ExitError); ok && (exitErr.ExitCode() == 1 || exitErr.ExitCode() == 4) {
			return false, nil
		}
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// TestParseBotCommentStream_MultiPage verifies the Go-side stream parser used
// by fetchBotCommentsPaginated correctly decodes a stream of concatenated
// JSON objects shaped like `gh api --paginate --jq` output across several
// pages (no --slurp) — i.e. the replacement for the sort_by/last aggregation
// that used to happen inside jq, now that gh 2.97+ rejects
// `--paginate --slurp` combined with `--jq`.
func TestParseBotCommentStream_MultiPage(t *testing.T) {
	// Two "pages" worth of jq output concatenated with no separator, mirroring
	// how gh --paginate emits per-page --jq results back to back.
	stream := `{"author":"claude[bot]","body":"first page comment","posted_at":"2026-08-01T10:00:00Z","url":"https://example.com/1"}` +
		`{"author":"claude","body":"second page comment","posted_at":"2026-08-02T10:00:00Z","url":"https://example.com/2"}`

	comments, err := parseBotCommentStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d: %+v", len(comments), comments)
	}
	if comments[0].Body != "first page comment" || comments[1].Body != "second page comment" {
		t.Fatalf("comments decoded out of order or with wrong body: %+v", comments)
	}
	for _, c := range comments {
		if c.State != "posted" {
			t.Errorf("expected State=posted, got %q", c.State)
		}
	}
}

// TestParseBotCommentStream_Empty verifies an empty stream (no bot comments
// on any page) decodes to a non-nil, zero-length slice rather than erroring.
func TestParseBotCommentStream_Empty(t *testing.T) {
	comments, err := parseBotCommentStream(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comments == nil || len(comments) != 0 {
		t.Fatalf("expected empty non-nil slice, got %+v", comments)
	}
}

// TestParseBotCommentStream_Malformed verifies a truncated/invalid JSON
// object in the stream surfaces as an error rather than silently dropping
// comments.
func TestParseBotCommentStream_Malformed(t *testing.T) {
	_, err := parseBotCommentStream(strings.NewReader(`{"author":"claude[bot]","body":`))
	if err == nil {
		t.Fatal("expected error for malformed JSON stream")
	}
}

// TestLatestBotComment verifies "latest" selection picks the max posted_at
// across a multi-page-shaped set of comments — the cross-page aggregation
// that used to be jq's `sort_by(.created_at) | last` and now happens in Go
// after fetchBotCommentsPaginated collects every page.
func TestLatestBotComment(t *testing.T) {
	t.Run("empty returns nil", func(t *testing.T) {
		if got := latestBotComment(nil); got != nil {
			t.Fatalf("expected nil for empty input, got %+v", got)
		}
	})

	t.Run("picks max posted_at across out-of-order pages", func(t *testing.T) {
		comments := []BotCommentJSON{
			{Author: "claude[bot]", Body: "page 2, older-looking position", PostedAt: "2026-08-05T09:00:00Z"},
			{Author: "claude", Body: "page 1, actually latest", PostedAt: "2026-08-10T12:30:00Z"},
			{Author: "claude[bot]", Body: "page 1, oldest", PostedAt: "2026-08-01T00:00:00Z"},
		}
		latest := latestBotComment(comments)
		if latest == nil {
			t.Fatal("expected a non-nil result")
		}
		if latest.Body != "page 1, actually latest" {
			t.Fatalf("expected max posted_at comment, got %+v", latest)
		}
	})

	t.Run("single comment returns itself", func(t *testing.T) {
		comments := []BotCommentJSON{{Author: "claude", Body: "only one", PostedAt: "2026-08-01T00:00:00Z"}}
		latest := latestBotComment(comments)
		if latest == nil || latest.Body != "only one" {
			t.Fatalf("expected the single comment, got %+v", latest)
		}
	})
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
