export const meta = {
  name: 'acceptance-verify',
  description: 'Adversarially verify a plan\'s ## Acceptance criteria against OBSERVED behavior (workflow variant of the G5 stage)',
  whenToUse: 'Opt-in variant of skills/shared/acceptance-verify.md (features.extra.acceptance_verify_workflow). Same verdict-JSON contract as the single-agent path.',
  phases: [
    { title: 'Extract', detail: 'read the plan, list criteria' },
    { title: 'Verify', detail: 'one agent per criterion, real artifact' },
    { title: 'Refute', detail: 'skeptic per FAIL — false vetoes only' },
  ],
}

// args: { planFile: string, workdir: string, base: string }
// Returns the SAME contract as skills/shared/acceptance-verify.md:
//   {"verdict":"accepted|rejected|unverifiable","criteria":[{criterion,result,command,observed}],"notes":"..."}
// Tolerate args arriving as a JSON-encoded STRING (a documented caller pitfall).
let a = args
if (typeof a === 'string') {
  try { a = JSON.parse(a) } catch (e) { a = null }
}
const planFile = a && a.planFile
const workdir = (a && a.workdir) || ''
const base = (a && a.base) || 'homolog'
if (!planFile || !workdir) {
  return { verdict: 'unverifiable', criteria: [], notes: 'acceptance-verify.js requires args {planFile, workdir, base}' }
}

const HYGIENE = `READ-ONLY verification. Working directory: ${workdir} (use absolute paths in every tool call).
- Never Edit/Write tracked files, never touch .planning/, never commit, never spawn sub-agents.
- You MAY build the artifact to a mktemp scratch path (NEVER bin/bravros) and create throwaway fixture dirs under /tmp.
- Adversarial stance: you did not write this code and you do not believe it works. A green unit test is a claim, not evidence. A criterion is PASS only with a pasted command and its observed output.`

phase('Extract')
const extraction = await agent(
  `${HYGIENE}
Read the plan file ${planFile} and return its "## Acceptance" checklist items VERBATIM (strip only the leading "- [ ] "/"- [x] " marker). If the plan has NO ## Acceptance section, return an empty list.
Return JSON: {"criteria": ["...", ...]}. Example: {"criteria": ["\`bravros foo --bar\` exits 0"]}`,
  { label: 'extract:criteria', phase: 'Extract', schema: {
    type: 'object', required: ['criteria'],
    properties: { criteria: { type: 'array', items: { type: 'string' } } },
  } },
)

const criteria = (extraction && extraction.criteria) || []
if (criteria.length === 0) {
  // The LOUD escape hatch is the CALLER's job (acceptance-verify.md) — never a silent pass.
  return { verdict: 'unverifiable', criteria: [], notes: 'plan has no ## Acceptance section — nothing could be verified against observed behavior' }
}
log(`${criteria.length} criteria to verify`)

const RESULT_SCHEMA = {
  type: 'object', required: ['criterion', 'result', 'command', 'observed'],
  properties: {
    criterion: { type: 'string' },
    result: { type: 'string', enum: ['pass', 'fail', 'unverifiable'] },
    command: { type: 'string', description: 'exact command run, or "" if unverifiable' },
    observed: { type: 'string', description: 'what it actually printed / exit code' },
  },
}

const REFUTE_SCHEMA = {
  type: 'object', required: ['refuted', 'reason'],
  properties: { refuted: { type: 'boolean' }, reason: { type: 'string' } },
}

const verifyPrompt = (c) => `${HYGIENE}
You are verifying ONE acceptance criterion of the plan at ${planFile} (diff scope: git diff against origin/${base}).
Criterion (verbatim): ${c}
Derive an executable check, run it against the REAL artifact (build to a mktemp scratch path per skills/shared/smoke-gate.md when the criterion involves the CLI), and judge by OBSERVED OUTPUT — exit 0 with missing promised output/state is a FAIL.
Return JSON: {"criterion":"<verbatim>","result":"pass|fail|unverifiable","command":"<exact command>","observed":"<output/exit>"}.
Example: {"criterion":"x","result":"pass","command":"/tmp/t/bravros-smoke plan-lint p.md","observed":"exit=0, 1 warning"}`

// Verify + per-fail refute as ONE pipeline: each criterion flows independently
// (no barrier — a slow build on one criterion must not stall the others).
phase('Verify')
const results = await pipeline(
  criteria,
  (c) => agent(verifyPrompt(c), { label: `verify:${String(c).slice(0, 40)}`, phase: 'Verify', schema: RESULT_SCHEMA }),
  async (res, c) => {
    if (!res) return { criterion: String(c), result: 'unverifiable', command: '', observed: 'verifier agent died/skipped' }
    if (res.result !== 'fail') return res
    // Skeptic pass — FAIL-CLOSED ASYMMETRY: skeptics can only remove false
    // VETOES (a wrong fixture, a misread output); passes are never softened.
    const skeptic = await agent(
      `${HYGIENE}
A verifier claims this acceptance criterion FAILED. Try to REFUTE the fail — is the fixture wrong, the verb misused, the output misread? Re-run the check yourself if needed.
Criterion: ${res.criterion}
Verifier ran: ${res.command}
Verifier observed: ${res.observed}
Default to refuted:false when uncertain (an unrefuted fail STANDS).
Return JSON: {"refuted": true|false, "reason": "..."}. Example: {"refuted": false, "reason": "reproduced: same empty output"}`,
      { label: `refute:${String(res.criterion).slice(0, 36)}`, phase: 'Refute', schema: REFUTE_SCHEMA },
    )
    if (!skeptic || !skeptic.refuted) return res // fail stands
    // Refuted once -> ONE fresh re-run; a second fail stands regardless.
    const rerun = await agent(verifyPrompt(res.criterion) + `\nNOTE: a prior fail on this criterion was refuted as flawed (${skeptic.reason}). Verify from scratch with a correct setup.`, { label: `reverify:${String(res.criterion).slice(0, 34)}`, phase: 'Refute', schema: RESULT_SCHEMA })
    return rerun || res
  },
)

const clean = results.filter(Boolean)
const fails = clean.filter((r) => r.result === 'fail')
const unver = clean.filter((r) => r.result === 'unverifiable')
return {
  verdict: fails.length > 0 ? 'rejected' : 'accepted',
  criteria: clean,
  notes: fails.length > 0
    ? `${fails.length} confirmed fail(s) — see criteria[]`
    : (unver.length > 0 ? `accepted with ${unver.length} unverifiable criterion/criteria — read them before completion` : ''),
}
