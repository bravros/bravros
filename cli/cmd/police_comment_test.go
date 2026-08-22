package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runPreToolUseComment drives policePreToolUseCmd with a `bash` payload
// carrying command, and returns the parsed envelope printed to stdout. The
// process itself always exits 0 — exitCode == 2 inside the envelope is what
// actually blocks (see docs/cli/police.md), so tests assert on the envelope,
// never on RunE's error return.
func runPreToolUseComment(t *testing.T, command string) (envelope struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exitCode"`
}, raw string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	payload := `{"tool_name":"bash","tool_input":{"command":` + jsonString(command) + `}}`
	var out bytes.Buffer
	policePreToolUseCmd.SetIn(strings.NewReader(payload))
	policePreToolUseCmd.SetOut(&out)
	policePreToolUseCmd.SetErr(&bytes.Buffer{})

	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	raw = out.String()
	if raw != "" {
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatalf("failed to unmarshal envelope: %v, raw output: %q", err, raw)
		}
	}
	return
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// shellDoubleQuote wraps s in bash double quotes the way a real `--body
// "..."` invocation would appear on the command line: embedded newlines stay
// literal (commandSegments keeps them as part of the quoted token), and only
// characters bash itself treats specially inside double quotes are escaped.
// This is deliberately NOT json.Marshal — JSON escaping turns a newline into
// the two literal bytes `\` `n`, which is not what a real shell command
// contains and which commandSegments' backslash-escape handling would eat,
// silently mangling the body under test.
func shellDoubleQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\', '$', '`':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

func conformingBody() string {
	return commentOpening + "\n\n" + commentRequiredBlock
}

func ghPrCommentCmd(body string) string {
	return "gh pr comment 42 --body " + shellDoubleQuote(body)
}

func TestPoliceComment_ConformingBody_Passes(t *testing.T) {
	_, raw := runPreToolUseComment(t, ghPrCommentCmd(conformingBody()))
	if raw != "" {
		t.Errorf("expected conforming body to pass with no output, got %q", raw)
	}
}

func TestPoliceComment_ExtraInstructionsBetweenOpeningAndRequired_Passes(t *testing.T) {
	body := commentOpening + "\n\nAlso pay close attention to the new auth middleware.\n\n" + commentRequiredBlock
	_, raw := runPreToolUseComment(t, ghPrCommentCmd(body))
	if raw != "" {
		t.Errorf("expected body with extra instructions in the middle to pass, got %q", raw)
	}
}

func TestPoliceComment_ParaphrasedOpening_Blocks(t *testing.T) {
	body := "@claude please review this pull request and confirm if it is mergeable.\n\n" + commentRequiredBlock
	envelope, raw := runPreToolUseComment(t, ghPrCommentCmd(body))
	if raw == "" {
		t.Fatal("expected paraphrased opening to be blocked, got no output")
	}
	// Assert on the raw stdout content directly — the process always exits
	// rc=0, so exitCode:2 inside the printed JSON envelope is what actually
	// blocks (see runPreToolUseComment's doc comment).
	if !strings.Contains(raw, `"exitCode":2`) {
		t.Errorf("expected exitCode 2 in stdout, got %q", raw)
	}
	if !strings.Contains(raw, commentOpening) {
		t.Errorf("expected canonical template (with opening) in stdout for retry, got %q", raw)
	}
	if envelope.Exit != 2 {
		t.Errorf("exitCode = %d; want 2", envelope.Exit)
	}
	if !strings.Contains(envelope.Stderr, "Police Block") {
		t.Errorf("expected Police Block in stderr, got %q", envelope.Stderr)
	}
	if !strings.Contains(envelope.Stderr, commentOpening) {
		t.Errorf("expected canonical template (with opening) in stderr for retry, got %q", envelope.Stderr)
	}
}

func TestPoliceComment_MissingRequiredBlock_Blocks(t *testing.T) {
	body := commentOpening + "\n\nBRAVROS-VERDICT: approved\n"
	envelope, raw := runPreToolUseComment(t, ghPrCommentCmd(body))
	if raw == "" {
		t.Fatal("expected missing required block to be blocked, got no output")
	}
	if envelope.Exit != 2 {
		t.Errorf("exitCode = %d; want 2", envelope.Exit)
	}
	if !strings.Contains(envelope.Stderr, commentRequiredBlock) {
		t.Errorf("expected canonical template (with required block) in stderr for retry, got %q", envelope.Stderr)
	}
}

func TestPoliceComment_MangledRequiredBlock_Blocks(t *testing.T) {
	mangled := strings.Replace(commentRequiredBlock, "BRAVROS-VERDICT: approved", "BRAVROS-VERDICT: Approved", 1)
	body := commentOpening + "\n\n" + mangled
	envelope, raw := runPreToolUseComment(t, ghPrCommentCmd(body))
	if raw == "" {
		t.Fatal("expected mangled required block to be blocked, got no output")
	}
	if envelope.Exit != 2 {
		t.Errorf("exitCode = %d; want 2", envelope.Exit)
	}
}

func TestPoliceComment_BodyFile_Blocks(t *testing.T) {
	envelope, raw := runPreToolUseComment(t, "gh pr comment 42 --body-file review.md")
	if raw == "" {
		t.Fatal("expected --body-file invocation to be blocked, got no output")
	}
	if envelope.Exit != 2 {
		t.Errorf("exitCode = %d; want 2", envelope.Exit)
	}
	if !strings.Contains(envelope.Stderr, "use inline --body") && !strings.Contains(envelope.Stderr, "Use inline --body") {
		t.Errorf("expected guidance to use inline --body, got %q", envelope.Stderr)
	}
}

func TestPoliceComment_BodyFileStdin_Blocks(t *testing.T) {
	envelope, raw := runPreToolUseComment(t, "gh pr comment 42 -F -")
	if raw == "" {
		t.Fatal("expected -F - (stdin body-file) invocation to be blocked, got no output")
	}
	if envelope.Exit != 2 {
		t.Errorf("exitCode = %d; want 2", envelope.Exit)
	}
}

func TestPoliceComment_ClaudeCommentWithoutReviewWord_Passes(t *testing.T) {
	_, raw := runPreToolUseComment(t, ghPrCommentCmd("@claude can you take a look at this?"))
	if raw != "" {
		t.Errorf("expected @claude comment without 'review' to pass untouched, got %q", raw)
	}
}

func TestPoliceComment_NonClaudeComment_Passes(t *testing.T) {
	_, raw := runPreToolUseComment(t, ghPrCommentCmd("Thanks for the fix, merging shortly."))
	if raw != "" {
		t.Errorf("expected non-@claude comment to pass untouched, got %q", raw)
	}
}

func TestPoliceComment_StandDownSuppressesBlock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("BRAVROS_POLICE_STANDDOWN", "1")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	body := "@claude please review this PR, thanks."
	payload := `{"tool_name":"bash","tool_input":{"command":` + jsonString(ghPrCommentCmd(body)) + `}}`
	var out bytes.Buffer
	policePreToolUseCmd.SetIn(strings.NewReader(payload))
	policePreToolUseCmd.SetOut(&out)
	policePreToolUseCmd.SetErr(&bytes.Buffer{})

	if err := policePreToolUseCmd.RunE(policePreToolUseCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected standdown to suppress the comment cop, got %q", out.String())
	}
}

func TestPoliceComment_CRLFConformingBody_Passes(t *testing.T) {
	crlfBody := strings.ReplaceAll(conformingBody(), "\n", "\r\n")
	_, raw := runPreToolUseComment(t, ghPrCommentCmd(crlfBody))
	if raw != "" {
		t.Errorf("expected CRLF-normalized conforming body to pass, got %q", raw)
	}
}

// findRepoRoot walks up from the test's working directory looking for `.git`
// — the monorepo root, distinct from `cli/go.mod` which sits one level
// below it in a nested Go module. Fails the test rather than skipping when
// no `.git` is found, so a broken repo layout surfaces as a failure.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for i := 0; i < 20; i++ {
		if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate repo root (.git) by walking up from %s", dir)
	return ""
}

// briefingBashFenceRe matches the single ```bash fence in
// skills/pr-review/references/briefing.md that carries the canonical
// `gh pr comment` template.
var briefingBashFenceRe = regexp.MustCompile("(?s)```bash\n(.*?)\n```")

// TestPoliceCommentDrift asserts that the canonical comment template
// embedded in skills/pr-review/references/briefing.md — the source of truth
// a skill author edits — stays byte-identical to commentOpening and
// commentRequiredBlock in police_comment.go, which is what the PreToolUse
// hook actually enforces at tool-call time. Allowed normalization: CRLF->LF
// and outer whitespace trim ONLY. If the briefing file cannot be located,
// this fails loudly rather than skipping silently.
func TestPoliceCommentDrift(t *testing.T) {
	root := findRepoRoot(t)
	briefingPath := filepath.Join(root, "skills", "pr-review", "references", "briefing.md")
	raw, err := os.ReadFile(briefingPath)
	if err != nil {
		t.Fatalf("could not locate skills/pr-review/references/briefing.md at %s: %v", briefingPath, err)
	}

	content := strings.ReplaceAll(string(raw), "\r\n", "\n")

	m := briefingBashFenceRe.FindStringSubmatch(content)
	if m == nil {
		t.Fatal("could not find a ```bash fence in briefing.md")
	}
	fence := m[1]

	const bodyFlag = `--body "`
	idx := strings.Index(fence, bodyFlag)
	if idx == -1 {
		t.Fatal(`could not find --body "..." in briefing.md's bash fence`)
	}
	bodyRaw := fence[idx+len(bodyFlag):]
	bodyRaw = strings.TrimSuffix(bodyRaw, `"`)
	body := strings.TrimSpace(bodyRaw)

	const sep = "\n\n"
	splitIdx := strings.Index(body, sep)
	if splitIdx == -1 {
		t.Fatal("could not split briefing.md's template body into opening + required block")
	}
	gotOpening := body[:splitIdx]
	gotRequired := body[splitIdx+len(sep):]

	if gotOpening != commentOpening {
		t.Errorf("briefing.md opening drifted from commentOpening:\n got:  %q\nwant: %q", gotOpening, commentOpening)
	}
	if gotRequired != commentRequiredBlock {
		t.Errorf("briefing.md required block drifted from commentRequiredBlock:\n got:  %q\nwant: %q", gotRequired, commentRequiredBlock)
	}
}
