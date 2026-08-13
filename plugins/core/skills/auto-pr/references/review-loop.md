# Review loop — trigger, wait, verdict authority

Pre-check once: does a Claude review workflow exist? If not, skip the loop and report.

```bash
gh api repos/{owner}/{repo}/actions/workflows --jq '.workflows[] | select(.name | test("claude|Claude|CLAUDE")) | .id'
```

## 1. Trigger

The trailing sentinel block is **load-bearing for the merge gate** — the `BRAVROS-VERDICT:`
line is the only authoritative verdict input. It must be plain visible text: the @claude
GitHub Action strips HTML comments from posted reviews, so the old
`<!-- bravros-verdict -->` form never survived an Action run (B-0342). Keep both sentinel
lines byte-exact.

```bash
PR_NUM=$(gh pr view --json number -q '.number')
gh pr comment "$PR_NUM" --body "@claude review this PR and check if we are able to merge. Analyze the code changes for any issues, security concerns, or improvements needed.

Required: end your review with EXACTLY ONE of these lines, as plain text, alone on its own line, as the final line:

BRAVROS-VERDICT: approved
BRAVROS-VERDICT: changes-requested

Do NOT wrap the line in an HTML comment, code fence, blockquote, or list item — plain visible text only. Emit approved only if you would merge this PR as-is. A finding you consider non-blocking does not prevent approved. Any blocking finding requires changes-requested."
```

## 2. Wait (poll gh — max 30 min)

The @claude Action posts as login `"claude"`, **not** `"claude[bot]"` — filtering on the
wrong login silently returns nothing. Poll in background, ~60s intervals:

```bash
gh pr view "$PR_NUM" --json comments \
  --jq '[.comments[] | select(.author.login=="claude")] | last | .body' \
  | grep -E '^BRAVROS-VERDICT: (approved|changes-requested)$' | tail -1
```

> B-0174: the `Monitor` tool does NOT block inside subagent (`Agent` tool) contexts — it
> returns immediately. Use a background bash poll loop, never Monitor, when running as a subagent.

## 3. Verdict authority (two-tier, P-0183 G1)

- **`BRAVROS-VERDICT: approved`** — proceed.
- **`BRAVROS-VERDICT: changes-requested`** — standing veto **no token can override**. Fetch the review comments, categorize, dispatch fix agents, push, re-trigger. Max 3 cycles, then report remaining issues.
- **No marker** (prose-only review, or timeout) — the model must not guess a verdict; it fails closed. HALT and surface the escape hatch: operator runs `bravros pr-review unlock` from a **separate terminal outside Claude Code**, then `bravros pr-review "$PR_NUM" --write-stamp`. The session that wants the merge must never authorize its own merge.

