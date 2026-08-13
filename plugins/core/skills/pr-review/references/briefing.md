# pr-review Detailed Briefing

INTENT: post ONE verbatim `@claude` comment; the GitHub Action reviews asynchronously (~2–5 min)
and posts back to the PR. This skill never reviews, never polls, never merges.

PR number: `$ARGUMENTS` if numeric, else `gh pr view --json number -q .number`. None → STOP
("create one with /pr first"). Branch behind its base → rebase + `git push --force-with-lease`
first (nothing is under review yet); conflicts — interactive: ask; autonomous: proceed, note it.

## The comment — keep BYTE-EXACT (wire contract)

```bash
gh pr comment <PR_NUMBER> --body "@claude review this PR and check if we are able to merge. Analyze the code changes for any issues, security concerns, or improvements needed.

Required: end your review with EXACTLY ONE of these lines, as plain text, alone on its own line, as the final line:

BRAVROS-VERDICT: approved
BRAVROS-VERDICT: changes-requested

Do NOT wrap the line in an HTML comment, code fence, blockquote, or list item — plain visible text only. Emit approved only if you would merge this PR as-is. A finding you consider non-blocking does not prevent approved. Any blocking finding requires changes-requested."
```

The sentinel lines must stay plain visible text — the Action strips HTML comments, which is why
the old `<!-- bravros-verdict -->` form never survived. Comment fails → STOP and report.

## Verdict authority — sentinel over prose

- **Tier 1 — the visible `BRAVROS-VERDICT:` line is authoritative both ways:** `approved` allows
  the merge-gate stamp; `changes-requested` is a standing veto, NOT token-overridable.
- **Tier 2 — prose is report-only.** A verdict inferred from free-form prose never authorizes a
  merge — conditional sign-offs ("LGTM assuming you fix X") make prose classifiers structurally
  unfixable. Even a clear "Mergeable." writes no stamp.
- The Action posts under login `claude`, **not** `claude[bot]` — filter on the wrong one and you
  silently read nothing.

## Stamp authority — `bravros pr-review --write-stamp` is the ONE writer

`bravros pr-review "$PR" --write-stamp` re-parses the live bot verdict and writes
`.planning/.review-stamp-<PR>.json` ONLY on a sentinel `approved`; anything else is a safe no-op.
No skill, session, or bash redirect ever hand-writes a stamp. Marker-less review the operator
agrees with? Authority comes out-of-band: `bravros pr-review unlock` in a **separate terminal**
(Claude Code CANNOT mint it — the session must not authorize its own merge; 5-min TTL,
single-use), then re-run `--write-stamp`. One approval buys exactly one merge. Never loop or
re-trigger the review to force a marker.

## After posting

- Autonomous (`.planning/.auto-*-lock` matches): print `STATUS: review-triggered. NEXT: wait for stamp`, return.
- Interactive: announce, then tell the user to run `/address-pr` when the review lands. Do not
  poll or inspect logs — a misfired workflow means the user reruns `/pr-review`.

```bash
bravros ha say --force "Revisão $PR_NUMBER aguardando análise remota. Projeto $(basename "$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")")." >/dev/null 2>&1 || true
```
