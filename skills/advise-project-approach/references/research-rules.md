# Research rules — evidence, freshness, comparables

## Capability routing

Name which of these are actually available before researching, and disclose the gaps rather than implying breadth: local repo/git-history inspection · official docs and pricing pages · GitHub repo and issue search · community sources (X / Reddit / YouTube) only if the user opted in. Never describe a multi-source search when one source was checked.

If browsing is unavailable, continue on local evidence and say external benchmarking was not performed.

## Evidence ledger

Keep it compact while researching; it becomes the `Evidence Reviewed` section.

| Field | Content |
|---|---|
| Claim or decision | what this evidence is being used to decide |
| Source | local file/command, or external URL |
| Observed | exact date, for anything time-sensitive |
| Support | what the source actually establishes |
| Limit | what it does **not** establish |

## Stop rule

Start with the smallest evidence set that could change the decision: 2–3 direct or adjacent comparables, primary documentation per material stack claim, the official pricing/limits page per cost-sensitive claim, and one contrasting alternative when it clarifies the choice.

Stop when every material recommendation is supported, the main alternative is understood, and the remaining uncertainty is listed explicitly.

## Freshness

- Exact dates for updates, releases, maintenance, or "recent" guidance.
- Never "as of 2025", "current", "latest", "active", "maintained" unless browsing or local git metadata verified it — add "visible at time of review" plus the observed date.
- Star counts, downloads, release dates, and last-commit dates are all time-sensitive.
- If a comparable inspired the recommendation but has since moved to a different stack, say so instead of flattening it into the older version.

## Source priority

Prefer primary sources: repository pages, official documentation, release notes, framework templates, standards, maintainer case studies, benchmark methodology pages. Treat blogs, rankings, and "best X" lists as weak unless they carry concrete evidence.

Record per external reference: URL · last-update or maintenance signal · adoption signal (stars, downloads, official status) · why it is relevant · limits of the comparison.

## Comparable selection

- At least one **direct** domain comparable when one exists.
- One **official template / reference architecture** when it would change the stack or architecture decision.
- One **contrasting heavier or lighter** alternative, so the recommendation reads as a fit test rather than a preference.

## Bias controls

- Never rank options by stars, social popularity, or visible adoption alone.
- State per comparable: what transfers, and what should not be copied.
- Heavy infrastructure in a mature comparable — decide whether it reflects real product needs or only that team's size, scale, history, or business model.
- When several popular comparables converge on one stack, still test it against the user's constraints and name a lighter plausible alternative.
- When the best fit is less popular than the visible comparables, say why fit beats popularity.
- When comparable research does **not** change the recommendation, say that too — confirming fit is a real result.
