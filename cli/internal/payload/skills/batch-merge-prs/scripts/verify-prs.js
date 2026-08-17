// Reusable named workflow — invoke with:
//   Workflow({ name: 'verify-prs', args: {
//     staging_branch: 'homolog',
//     mode: 'review' | 'deep',          // default 'review' (read-only, parallel).
//     bot: 'claude',                     // review-bot login to fetch (default 'claude' — F6).
//     guards: [ 'free-text guard rule', ... ],
//     prs: [ {pr, linked, kind, id, title, intent, files, superseded_hint?}, ... ]
//   } })
//
// review mode (DEFAULT) — READ-ONLY, PARALLEL: a fresh-eyes pre-merge verdict per PR. Mutates nothing
//   (no checkout, no merge, no edit, no tests) — the caller drives the serial batch merge orchestration around it.
//   Each PR also returns a polished `verification_body` markdown block the caller MAY append to the PR
//   body before triggering the bot review (F8).
//
// deep mode — SERIAL, MUTATING: for each PR an agent checks out the branch, runs the touched code's
//   whole-AREA tests via the project runner, and PROVES each net-new test with a red-green check
//   (revert the fix -> the new test goes red -> restore), then ALWAYS restores the staging branch.
//   A static code review cannot catch a RUNTIME regression — only an executed test can; deep mode is
//   that gate. It REQUIRES an exclusive working tree (NO operator suite running) — the caller gates
//   this per the skill's Working-tree-safety guard (#3). Runs serial because checkout mutates one tree.
//
// F6 — args.bot is the review-bot login to fetch (default 'claude'). The @claude Action posts as the
//   bare login "claude", NOT "claude[bot]" — filtering on "[bot]" alone silently returns nothing.
//   The fetch uses gh's SERVER-SIDE --jq: raw comment bodies contain control chars that crash naive
//   local JSON parsers (python json.load etc.).
//
// args.guards is an array of free-text instructions injected verbatim into EACH agent prompt — the seam
//   a project uses to enforce domain rules (a wire contract that must go to a human, a shared-path
//   regression rule, a compliance gate) WITHOUT this shared workflow hard-coding project specifics.
// args.staging_branch is the branch every PR targets (default 'homolog').
// args.prs[].kind: 'issue' | 'backlog'   args.prs[].id: e.g. '228' or 'B-0147'
// args.prs[].superseded_hint: OPTIONAL — for pre-existing PRs flagged "already-done-partial" so the agent
//   diffs PR-vs-staging and reports what is genuinely net-new (verdict 'superseded' when intent landed).

export const meta = {
  name: 'verify-prs',
  description: 'Pre-merge verify of N PRs vs the merge-bar + caller guard rules. review (read-only parallel) or deep (serial, runs area tests with a red-green proof).',
  whenToUse: 'Before merging a batch of PRs: get an independent per-PR verdict (clean / needs-changes / blocked / superseded). Use deep mode on an exclusive tree to catch runtime regressions a code review cannot.',
  phases: [{ title: 'Verify', detail: 'review: one code-reviewer per PR (parallel). deep: serial checkout + area tests + red-green.' }],
}

const PRS = (args && args.prs) || []
const STAGING = (args && args.staging_branch) || 'homolog'
const GUARDS = (args && args.guards) || []
const MODE = (args && args.mode) || 'review'
const BOT = (args && args.bot) || 'claude'
if (!PRS.length) {
  log('verify-prs: no PRs passed. Call with args:{staging_branch,mode,bot,guards,prs:[{pr,linked,kind,id,title,intent,files,superseded_hint?}]}')
  return { mode: MODE === 'deep' ? 'deep' : 'review', results: [] }
}

const guardBlock = GUARDS.length
  ? `\nPROJECT GUARD RULES — OUTRANK THE MERGE-BAR (apply every one; if a rule directs a verdict, that verdict wins):\n${GUARDS.map((g, i) => `  ${i + 1}. ${g}`).join('\n')}\nIf any guard rule is satisfied and directs a human-only hold, set verdict='blocked', recommendation='hold-human-guard', guard_triggered=true, and do NOT propose a fix or merge.\n`
  : '\nNo project guard rules were supplied — judge on the merge-bar alone.\n'

// ── review mode (default): read-only, parallel ──────────────────────────────────────────────
const SCHEMA = {
  type: 'object',
  additionalProperties: true,
  required: ['pr','verdict','recommendation'],
  properties: {
    pr: { type: 'number' },
    verdict: { type: 'string', enum: ['clean','needs-changes','blocked','superseded'] },
    recommendation: { type: 'string', enum: ['merge','address-then-merge','hold-human-guard','close-superseded'] },
    guard_triggered: { type: 'boolean' },
    already_on_staging: { type: 'boolean' },
    net_new: { type: 'string' },
    linked_intent_satisfied: { type: 'boolean' },
    bot_review_verdict: { type: 'string' },
    mergeable: { type: 'string' },
    blocking: { type: 'array', items: { type: 'object', additionalProperties: true, required: ['file','issue'], properties: {
      file: { type: 'string' }, line: { type: 'string' }, issue: { type: 'string' }, suggested_fix: { type: 'string' } } } },
    affected_tests: { type: 'array', items: { type: 'string' } },
    verification_body: { type: 'string' },
    notes: { type: 'string' },
  },
}

const reviewPrompt = (p) => `You are giving PR #${p.pr}${p.title ? ' ("' + p.title + '")' : ''} a FRESH-EYES, READ-ONLY pre-merge review. Working dir is the repo root. The PR targets the staging branch "${STAGING}".

LINKED INTENT (${p.linked}): ${p.intent}
${p.superseded_hint ? '\nSUPERSEDED CHECK (this PR was flagged "already-done-partial" — verify against CURRENT ' + STAGING + ' HEAD): ' + p.superseded_hint + '\n' : ''}
CHANGED FILES (full set — the change must be contained to these):
${(p.files || []).map(f => '  - ' + f).join('\n')}

DO THIS (read-only — never edit, never checkout, never merge, never run tests):
1. Diff the PR:  gh pr diff ${p.pr}
2. For each changed PRODUCTION file, read its current staging state (git show origin/${STAGING}:<path>) and determine what in the PR is NET-NEW vs already-landed. Set already_on_staging=true ONLY if the PR's substantive intent is already satisfied on ${STAGING}.
2b. Blast radius (graph-enabled repos only — graphify-out/graph.json or .graphify present):
    \`graphify query "what calls <the changed symbol>"\` — a caller outside the CHANGED FILES list
    that the diff breaks is a REAL blocking finding, not a nit. The graph is a hint: confirm each
    caller with \`git grep -F\` before you block on it.
3. Fetch the latest bot/code review on the PR (may be empty/stale — record bot_review_verdict as approved | no-new-blockers | changes-requested | unclear | none). Use gh's SERVER-SIDE --jq (raw comment bodies contain control chars that crash local JSON parsers), matching the bare login "${BOT}" OR any "*[bot]" — the @claude Action posts as bare "${BOT}", not "${BOT}[bot]":  gh pr view ${p.pr} --json comments --jq '[.comments[] | select(((.author.login // "") == "${BOT}") or ((.author.login // "") | endswith("[bot]")))] | last | .body // "none"'
4. Read the linked intent: ${p.kind === 'issue' ? 'gh issue view ' + p.id : 'grep -rl "' + p.id + '" .planning/backlog/ and read the matching item file'}
5. Record mergeable: gh pr view ${p.pr} --json mergeable,mergeStateStatus -q '"\\(.mergeable) \\(.mergeStateStatus)"'
${guardBlock}
MERGE-BAR ("comes clean, no nits" — cosmetic/style/naming/optional suggestions are IGNORED, never lower the verdict):
- 'superseded' + 'close-superseded': substantive intent already on ${STAGING} and the net-new remainder is not worth merging. Explain in net_new what (if anything) is lost by closing.
- 'clean' + 'merge': net-new, fully satisfies the linked intent, contained to the listed files, mergeable, no real blocker, no guard triggered, bot review (if present) not changes-requested.
- 'needs-changes' + 'address-then-merge': >=1 REAL blocking finding (logic bug, security hole, broken/wrong/missing test for a net-new branch, merge conflict) fixable with a contained change. List each in blocking with file/line/problem/precise suggested_fix.
- 'blocked' + 'hold-human-guard': a project guard rule directs a human-only hold.

verification_body: write a polished markdown block titled '## Verification' (≤12 lines) the caller can paste into the PR body before the bot review — what you checked, the net-new summary, mergeability, and the verdict. Plain markdown, no code fences around the whole block, no AI attribution.

Be adversarial — default to refuting 'clean'. Set affected_tests to the test files among the changed files. Return ONLY the structured object; put full reasoning (net-new analysis, any guard determination with specific lines, mergeability, blocking rationale) in notes.

One-shot example (correct minimal return):
{"pr":${p.pr},"verdict":"clean","recommendation":"merge","guard_triggered":false,"already_on_staging":false,"net_new":"Adds tenant scope to order queries","linked_intent_satisfied":true,"bot_review_verdict":"approved","mergeable":"MERGEABLE CLEAN","blocking":[],"affected_tests":["tests/Feature/OrderScopeTest.php"],"verification_body":"## Verification\\n- Diffed PR vs staging: net-new tenant scope\\n- Mergeable: clean\\n- Verdict: clean → merge","notes":"No blockers found."}
If your JSON is rejected, re-emit using the validator error verbatim — output only the corrected JSON.`

// ── deep mode: serial, mutates the tree, runs the touched area's tests with a red-green proof ──
const DEEP_SCHEMA = {
  type: 'object',
  additionalProperties: true,
  required: ['pr','verdict','recommendation'],
  properties: {
    pr: { type: 'number' },
    verdict: { type: 'string', enum: ['clean','needs-changes','blocked','superseded'] },
    recommendation: { type: 'string', enum: ['merge','address-then-merge','hold-human-guard','close-superseded'] },
    guard_triggered: { type: 'boolean' },
    test_command: { type: 'string' },
    tests_passed: { type: 'boolean' },
    red_green_proof: { type: 'string' },
    restored_staging: { type: 'boolean' },
    blocking: { type: 'array', items: { type: 'object', additionalProperties: true, required: ['file','issue'], properties: {
      file: { type: 'string' }, line: { type: 'string' }, issue: { type: 'string' }, suggested_fix: { type: 'string' } } } },
    notes: { type: 'string' },
  },
}

const deepPrompt = (p) => `You are running a DEEP, RUNTIME pre-merge verification of PR #${p.pr}${p.title ? ' ("' + p.title + '")' : ''} on an EXCLUSIVE working tree (no other suite is running — the caller guaranteed this). Working dir is the repo root. The PR targets "${STAGING}".

LINKED INTENT (${p.linked}): ${p.intent}
CHANGED FILES:
${(p.files || []).map(f => '  - ' + f).join('\n')}

CRITICAL — you MUST restore the staging branch when done (even on failure/error). Record restored_staging=true only after \`git checkout ${STAGING}\` succeeds at the end.

DO THIS (serial; you own the tree for this PR):
1. Record where you are:  START=$(git rev-parse --abbrev-ref HEAD)  (expect ${STAGING}).
2. Check out the PR:  gh pr checkout ${p.pr}   (resolves the head branch).
3. Identify the touched code's WHOLE AREA — not just the changed files. A regression can live in a sibling test that exercises the same service/module. If the repo has a graph (graphify-out/graph.json or .graphify), scope that area with \`graphify query "what depends on <each changed file's main symbol>"\` from the repo root instead of guessing from directory layout — then verify each dependent in source before running its tests; no graph → fall back to grep + directory siblings. Use the project's own test runner (read its lockfiles / CLAUDE.md — never assume a binary); run the area's tests, capturing the exact command in test_command and the pass/fail in tests_passed.
4. RED-GREEN PROOF for each NET-NEW test this PR adds: temporarily revert the production fix it covers (git stash / targeted edit), re-run that test, CONFIRM it goes RED, then restore the fix and confirm GREEN. Describe each red-green in red_green_proof. If the PR adds no net-new test, set red_green_proof to "n/a — no net-new test" and treat a net-new production branch with no test as a blocking finding.
5. ALWAYS restore:  git checkout ${STAGING}  (and \`git stash pop\`/clean any temp reverts first). Set restored_staging accordingly.
${guardBlock}
VERDICT (same bar as review mode, plus the runtime evidence):
- 'clean' + 'merge': area tests pass, every net-new test red-green-proven, no guard triggered.
- 'needs-changes' + 'address-then-merge': a failing area test, a missing test for a net-new branch, or a red-green that did not go red. List each in blocking.
- 'blocked' + 'hold-human-guard': a project guard rule directs a human-only hold.

Return ONLY the structured object; put the full test transcript reasoning in notes.

One-shot example (correct minimal return):
{"pr":${p.pr},"verdict":"clean","recommendation":"merge","guard_triggered":false,"test_command":"vendor/bin/pest tests/Feature/Orders","tests_passed":true,"red_green_proof":"Reverted the fix → OrderScopeTest went RED; restored → GREEN","restored_staging":true,"blocking":[],"notes":"Area suite green."}
If your JSON is rejected, re-emit using the validator error verbatim — output only the corrected JSON.`

phase('Verify')

if (MODE === 'deep') {
  // SERIAL — checkout mutates the single shared tree; never parallelize deep mode.
  const out = []
  for (const p of PRS) {
    const r = await agent(deepPrompt(p), { schema: DEEP_SCHEMA, label: `deep:#${p.pr}`, phase: 'Verify', effort: 'high', agentType: 'general-purpose' })
    out.push(r)
  }
  return { mode: 'deep', results: out.filter(Boolean) }
}

const results = await parallel(PRS.map(p => () =>
  agent(reviewPrompt(p), { schema: SCHEMA, label: `verify:#${p.pr}`, phase: 'Verify', effort: 'high', agentType: 'code-reviewer' })
))

return { mode: 'review', results: results.filter(Boolean) }
