// Reusable named workflow — the parallel-investigate + adversarial-certify engine of /scout.
//   Workflow({ name: 'scout-investigate', args: {
//     scout_dir: '.planning/debug/S-0007-orders-total-open',  // REQUIRED — the reserved S-NNNN dir (Step 2)
//     bug: 'OrderService::total() returns the pre-discount total',  // the symptom under investigation
//     category: 'unexpected-behavior',      // test-failure | runtime-error | unexpected-behavior | performance | integration
//     stack: 'laravel',                     // framework / language string (detected from repo manifests)
//     repro: 'php artisan test --filter OrderTest::total',  // OPTIONAL — a reproduction path; presence dispatches repro-verifier
//     leads: [ { source:'graphify'|'grep'|'git'|'error', ref:'app/Services/OrderService.php:88', note:'...' }, ... ],
//     boost: false,                         // true when Laravel Boost MCP is connected (richer runtime probes)
//     max_rounds: 3,                         // certify round cap (default 3)
//   } })
//
// READ-ONLY end to end. Owns /scout Steps 4-6 ONLY: Step 4 (parallel verification agents),
// Step 5 (synthesize one falsifiable hypothesis), Step 6 (adversarially certify it with runtime
// evidence, looping back to a fresh investigation round on refutation, capped at max_rounds).
// The skill keeps Steps 1-3 (gate + reserve dir + leads) and Steps 7-8 (report + handoff) as prose.
//
// Returns { rounds, certified, root_cause, confidence, hypothesis, certification, findings_files, notes }.
// The caller writes diagnosis.md / report.md from this payload and drives the mandatory handoff.
//
// Each verification agent WRITES its findings file ($SCOUT_DIR/agent-N-findings.md) AND returns the
// structured FINDINGS object — the file is the durable artifact, the object is what the workflow
// synthesizes on. code-tracer -> agent-1, blast-radius-mapper -> agent-2, repro-verifier -> agent-3.

export const meta = {
  name: 'scout-investigate',
  description: 'Parallel verify of a bug lead list (code-tracer + blast-radius-mapper, +repro-verifier when a repro path exists) then adversarial runtime certify of one hypothesis, looping <=3 rounds. Read-only.',
  whenToUse: 'Inside /scout Steps 4-6: verify leads against real source in parallel, synthesize one falsifiable scout hypothesis, and certify it with runtime evidence before any diagnosis is written — refuted hypotheses loop into a fresh round.',
  phases: [
    { title: 'Investigate', detail: 'code-tracer + blast-radius-mapper (always) + repro-verifier (when repro path exists), parallel, read-only; each writes agent-N-findings.md.' },
    { title: 'Certify', detail: 'one repro-verifier adversarially proves/refutes the synthesized hypothesis with runtime evidence; refutation carries into the next round (<=3).' },
  ],
}

const SCOUT_DIR = (args && args.scout_dir) || ''
const BUG = (args && args.bug) || ''
const CATEGORY = (args && args.category) || 'unexpected-behavior'
const STACK = (args && args.stack) || 'unknown'
const REPRO = (args && args.repro) || ''
const LEADS = (args && args.leads) || []
const BOOST = !!(args && args.boost)
const MAX_ROUNDS = (args && args.max_rounds) || 3

if (!SCOUT_DIR || !BUG) {
  log('scout-investigate: needs args:{scout_dir, bug, category, stack, repro?, leads:[{source,ref,note}], boost?, max_rounds?}')
  return { rounds: 0, certified: false, root_cause: '', confidence: 'low', hypothesis: '', certification: '', findings_files: [], notes: 'missing scout_dir or bug' }
}

const leadBlock = LEADS.length
  ? LEADS.map((l, i) => `  ${i + 1}. [${l.source || 'lead'}] ${l.ref || ''}${l.note ? ' — ' + l.note : ''}`).join('\n')
  : '  (no structured leads passed — derive candidate sites from the bug text, error frames, git history, and grep)'

const boostNote = BOOST
  ? 'Runtime probes: DEFAULT to php artisan one-shots via Bash (tinker --execute=\'…\', db:table, route:list --json, tail storage/logs/laravel.log) — read-only only. Laravel Boost MCP is also connected; its tools are acceptable equivalents but artisan is cheaper.'
  : 'Runtime probes: use php artisan one-shots via Bash (tinker --execute=\'…\', db:table, route:list --json, tail storage/logs/laravel.log) — read-only only. No Boost MCP in this session; do not ask for it.'

// ── Step 4: per-angle verification findings ─────────────────────────────────────────────────
const FINDINGS_SCHEMA = {
  type: 'object',
  additionalProperties: true,
  required: ['agent', 'most_likely_cause', 'confidence', 'evidence'],
  properties: {
    agent: { type: 'string', enum: ['code-tracer', 'blast-radius-mapper', 'repro-verifier'] },
    angle: { type: 'string' },
    findings_file: { type: 'string' },
    leads_verified: { type: 'array', items: { type: 'object', additionalProperties: false, required: ['lead', 'verdict', 'evidence'], properties: {
      lead: { type: 'string' }, verdict: { type: 'string', enum: ['confirmed', 'refuted', 'inconclusive'] }, evidence: { type: 'string' } } } },
    most_likely_cause: { type: 'string' },
    confidence: { type: 'string', enum: ['high', 'medium', 'low'] },
    evidence: { type: 'string' },
    would_certify: { type: 'string' },
    fix_direction: { type: 'string' },
    notes: { type: 'string' },
  },
}

// ── Step 6: adversarial certification verdict ───────────────────────────────────────────────
const CERTIFY_SCHEMA = {
  type: 'object',
  additionalProperties: true,
  required: ['certified', 'proof_kind'],
  properties: {
    certified: { type: 'boolean' },
    proof_kind: { type: 'string', enum: ['reproduce-state-match', 'counterfactual', 'evidence-chain', 'none'] },
    proof_transcript: { type: 'string' },
    refutation: { type: 'string' },
    residual_doubt: { type: 'string' },
    notes: { type: 'string' },
  },
}

// One-shot examples echoed into every schema'd prompt — the #1 schema-failure fix: show the exact
// expected shape once. On host validation failure the retry prompt interpolates the validator
// message verbatim (host-side behavior).
const ONE_SHOT_FINDINGS = `Return ONLY JSON. Example: {"agent":"code-tracer","angle":"query path","findings_file":".debug/trace.md","leads_verified":[{"lead":"stale cache","verdict":"refuted","evidence":"cache cleared, bug persists"}],"most_likely_cause":"missing tenant scope in query","confidence":"high","evidence":"query log shows unscoped select","would_certify":"yes","fix_direction":"add tenant scope","notes":""}`

const ONE_SHOT_CERTIFY = `Return ONLY JSON. Example: {"certified":false,"proof_kind":"none","proof_transcript":"OrderTest::total passes after cache clear — hypothesis value never observed","refutation":"runtime state contradicts the predicted mechanism","residual_doubt":"","notes":""}`

const readOnly = `HARD CONSTRAINTS (read-only investigation — the parent /scout skill is read-only by audit Rule 22):
- NEVER modify, create, or delete application files. The ONLY files you may write are inside ${SCOUT_DIR}.
- Run only READ-ONLY commands: grep/find/ripgrep, git log/diff/blame, the test runner with a filter (to reproduce), a REPL/tinker/query (to inspect). NEVER run mutating commands — migrations, shared-cache clears, data writes, side-effecting job dispatch.
- ${boostNote}
- Do NOT spawn sub-agents. Do all the work yourself.`

const tracePrompt = (round, refutation) => `You are code-tracer (Agent A — Code Trace) verifying a bug in a ${STACK} project. Working dir is the repo root. Bug category: ${CATEGORY}.

BUG: ${BUG}
INVESTIGATION DIR: ${SCOUT_DIR}
${round > 1 ? '\nThis is investigation ROUND ' + round + '. A prior hypothesis was REFUTED by runtime evidence — do NOT re-tread it:\n' + refutation + '\nFind a DIFFERENT failure site.\n' : ''}
LEAD LIST (candidates to verify against REAL source — never conclusions, each tagged by source):
${leadBlock}

Open the actual source for each lead and trace the real execution path entry point -> suspected fault, confirming or refuting each lead line by line. The territory wins: when a graphify-tagged lead and the source disagree, the source is truth — say so. Read what the code does; never infer behavior from a function name. git blame the suspect lines — recent changes are prime suspects. Refuting a lead is a valuable result.

${readOnly}

WRITE your findings to ${SCOUT_DIR}/agent-1-findings.md (leading with the ordered entry->fault call chain, then Leads Verified, then a single falsifiable Hypothesis with file:line + mechanism, then Suggested Fix Direction). THEN return the structured object: set findings_file='${SCOUT_DIR}/agent-1-findings.md', agent='code-tracer'. Code reading yields a hypothesis, not proof — fill would_certify with the runtime check that would PROVE it. Put the full call chain in notes.`

const blastPrompt = (round, refutation) => `You are blast-radius-mapper (Agent B — Call-Site & Blast Radius) for a bug in a ${STACK} project. Working dir is the repo root. Bug category: ${CATEGORY}.

BUG: ${BUG}
INVESTIGATION DIR: ${SCOUT_DIR}
${round > 1 ? '\nThis is investigation ROUND ' + round + '. A prior hypothesis was REFUTED:\n' + refutation + '\nWiden the search to call paths the refuted hypothesis ignored.\n' : ''}
LEAD LIST (candidates to verify against REAL source — each tagged by source):
${leadBlock}

For each suspect symbol/function/file, find EVERY place that depends on it from THREE independent sources and cross-check them: (a) graphify neighbors if a .graphify graph is present (HINT only), (b) ripgrep/grep across the whole codebase, (c) framework-aware sites — routes, event listeners, queue jobs, scheduled commands, DI bindings, config references, Blade/template usage, middleware, observers. Any caller grep/framework-search finds that graphify MISSED is proof the graph is stale — record it as 'graphify stale: missed <caller>'. For each call site note whether it passes the inputs that trigger the bug and what shared state (DB columns, cache keys, session, globals, config) the suspect code reads/writes. Judge whether the impact is isolated to one path or cross-cutting.

${readOnly}

WRITE your findings to ${SCOUT_DIR}/agent-2-findings.md (Direct Callers, Transitive Dependents, Shared State Touched, Risk Assessment, one falsifiable Hypothesis with file:line, Suggested Fix Direction). THEN return the structured object: set findings_file='${SCOUT_DIR}/agent-2-findings.md', agent='blast-radius-mapper'. Put the risk assessment + stale-graph notes in notes.`

const reproFindPrompt = (round, refutation) => `You are repro-verifier (Agent C — Reproduce & Runtime Evidence) for a bug in a ${STACK} project. Working dir is the repo root. Bug category: ${CATEGORY}.

BUG: ${BUG}
INVESTIGATION DIR: ${SCOUT_DIR}
REPRODUCTION PATH: ${REPRO}
${round > 1 ? '\nThis is investigation ROUND ' + round + '. A prior hypothesis was REFUTED:\n' + refutation + '\nCapture the runtime values that distinguish the new candidate cause.\n' : ''}
LEAD LIST (each tagged by source):
${leadBlock}

Make the bug ACTUALLY HAPPEN and capture concrete runtime values. Identify the smallest reliable reproduction (the named failing test / repro steps / a one-off invocation), run it, capture the FULL output verbatim — assertion failure, exception class, stack trace, exit code. At the failure point inspect runtime state with READ-ONLY probes (REPL/tinker/a focused query/printing a value); record concrete values, not adjectives. Correlate with logs and any observability surface (log files, Sentry MCP if present) — first-seen vs recurrence. State observed vs expected with concrete numbers. A clean NON-reproduction is a valid, load-bearing result — if it will not reproduce, say so plainly.

${readOnly}

WRITE your findings to ${SCOUT_DIR}/agent-3-findings.md (Repro Steps, Observed vs Expected, verbatim Runtime Evidence, confirmed true/false, one falsifiable Hypothesis with file:line, Suggested Fix Direction). THEN return the structured object: set findings_file='${SCOUT_DIR}/agent-3-findings.md', agent='repro-verifier'. Put the verbatim runtime transcript in evidence.`

const certifyPrompt = (round, hypothesis, findingsSummary) => `You are an ADVERSARIAL certification agent (repro-verifier role) for /scout Step 6, ${STACK} project. Working dir is the repo root. This is round ${round} of at most ${MAX_ROUNDS}.

A code-reading hypothesis fits the source. Your job is to decide whether it is what ACTUALLY HAPPENS AT RUNTIME — default to REFUTING it. A guess dressed up as a diagnosis is worse than none.

HYPOTHESIS TO CERTIFY (one falsifiable claim — file, line, mechanism):
${hypothesis}

WHAT THE INVESTIGATION ROUND FOUND:
${findingsSummary}
${REPRO ? '\nREPRODUCTION PATH: ' + REPRO + '\n' : ''}
Certify ONLY when you hold at least one of these proofs backed by REAL runtime output you can paste:
  1. reproduce-state-match — reproduce the failure deterministically, inspect runtime state at the failure point, confirm it matches the hypothesis EXACTLY.
  2. counterfactual — exercise the suspect logic in isolation; it produces the wrong result for the predicted reason, and the correct result once the predicted condition is removed.
  3. evidence-chain — symptom -> ... -> cause where EVERY link is confirmed against real source AND real runtime data, no "probably", no skipped step.
Code reading ALONE is not certification.

${readOnly}

DECIDE:
- If a proof holds: certified=true, proof_kind set, proof_transcript = the ACTUAL runtime output (query results / tinker output / test transcript), refutation='', residual_doubt = any caveat.
- If the runtime evidence CONTRADICTS the hypothesis, it will not reproduce, or a chain link breaks: certified=false, proof_kind='none', refutation = a precise statement of WHY the evidence killed it (so the next investigation round does not re-tread it), proof_transcript = the runtime output that refuted it.
Return ONLY the structured object; put full reasoning in notes.`

phase('Investigate')

let round = 0
let certified = false
let hypothesis = ''
let confidence = 'low'
let certification = ''
let refutation = ''
const findingsFiles = []
const refutationLog = []

for (round = 1; round <= MAX_ROUNDS; round++) {
  // Step 4 — parallel verification agents. code-tracer + blast-radius-mapper ALWAYS;
  // repro-verifier ONLY when a reproduction path was supplied.
  const tasks = [
    () => agent(tracePrompt(round, refutation) + '\n\n' + ONE_SHOT_FINDINGS, { schema: FINDINGS_SCHEMA, label: `trace:r${round}`, phase: 'Investigate', effort: 'high', agentType: 'code-tracer' }),
    () => agent(blastPrompt(round, refutation) + '\n\n' + ONE_SHOT_FINDINGS, { schema: FINDINGS_SCHEMA, label: `blast:r${round}`, phase: 'Investigate', effort: 'high', agentType: 'blast-radius-mapper' }),
  ]
  if (REPRO) {
    tasks.push(() => agent(reproFindPrompt(round, refutation) + '\n\n' + ONE_SHOT_FINDINGS, { schema: FINDINGS_SCHEMA, label: `repro:r${round}`, phase: 'Investigate', effort: 'high', agentType: 'repro-verifier' }))
  }

  const findings = (await parallel(tasks)).filter(Boolean)
  for (const f of findings) {
    if (f.findings_file && !findingsFiles.includes(f.findings_file)) findingsFiles.push(f.findings_file)
  }

  // Step 5 — synthesize ONE falsifiable hypothesis. Prefer the highest-confidence agent claim;
  // a high-confidence claim outranks medium outranks low.
  const rank = { high: 3, medium: 2, low: 1 }
  const lead = findings.slice().sort((a, b) => (rank[b.confidence] || 0) - (rank[a.confidence] || 0))[0]
  if (!lead || !lead.most_likely_cause) {
    // Divergent / all-refuted — no hypothesis this round; carry forward and loop.
    refutation = `Round ${round}: agents produced no convergent hypothesis (all leads killed or contradictory). Try a fresh lead angle.`
    refutationLog.push(refutation)
    continue
  }
  hypothesis = lead.most_likely_cause
  confidence = lead.confidence

  const findingsSummary = findings
    .map(f => `- [${f.agent}] (${f.confidence}) ${f.most_likely_cause} | evidence: ${f.evidence} | would certify: ${f.would_certify ?? 'n/a'}`)
    .join('\n')

  // Step 6 — adversarial certification PROOF GATE.
  phase('Certify')
  const verdict = await agent(certifyPrompt(round, hypothesis, findingsSummary) + '\n\n' + ONE_SHOT_CERTIFY, { schema: CERTIFY_SCHEMA, label: `certify:r${round}`, phase: 'Certify', effort: 'high', agentType: 'repro-verifier' })

  if (verdict && verdict.certified) {
    certified = true
    certification = verdict.proof_transcript || ''
    break
  }

  // Refuted — caught a fake root cause before it cost a fix cycle. Carry WHY into the next round.
  refutation = `Round ${round} REFUTED hypothesis "${hypothesis}": ${(verdict && verdict.refutation) || 'runtime evidence contradicted it'}`
  refutationLog.push(refutation)
  phase('Investigate')
}

const rounds = certified ? round : MAX_ROUNDS

return {
  rounds,
  certified,
  root_cause: certified ? hypothesis : (hypothesis ? `UNCERTIFIED — ${hypothesis}` : 'UNCERTIFIED — no convergent hypothesis'),
  confidence: certified ? confidence : 'low',
  hypothesis,
  certification,
  findings_files: findingsFiles,
  notes: certified
    ? `Certified in round ${rounds} of ${MAX_ROUNDS}.`
    : `Round cap (${MAX_ROUNDS}) hit without certification. Refutation trail:\n${refutationLog.join('\n')}`,
}
