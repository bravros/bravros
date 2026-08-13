// Reusable named workflow — drain the open-issue + backlog queue by classifying each item vs LIVE code.
//   Workflow({ name: 'triage-sweep' })                                          // zero-arg: gathers all open issues + active backlog
//   Workflow({ name: 'triage-sweep', args: { staging_branch: 'homolog', guards: [ 'free-text rule', ... ] } })
//   Workflow({ name: 'triage-sweep', args: { issues:[{id,title}], backlog:[{id,title,path}] } })  // explicit override
//
// READ-ONLY: dedup -> classify each item -> adversarially verify every close-eligible verdict.
// Returns {dedup, results}. The CALLER (the triage-sweep skill) applies all closes/cancels SERIALLY
// (parallel writers on the shared .planning/events.jsonl ledger interleave/corrupt it).
//
// args.guards : free-text rules injected verbatim into EVERY classifier — the seam a project uses to
//   force domain verdicts (e.g. "If the change touches the wire contract, verdict='human-only-integration'")
//   WITHOUT this shared workflow hard-coding any project specifics.
// args.staging_branch : the branch whose HEAD is "current truth" (default 'homolog').

export const meta = {
  name: 'triage-sweep',
  description: 'Read-only triage of every open issue + backlog item vs live code: dedup, classify, adversarially verify every close. Caller applies closes serially.',
  whenToUse: 'To drain a stale GitHub-issue + backlog queue: find what is already implemented / no longer needed, with an independent verifier gating every auto-close. Run before any fix sweep.',
  phases: [{ title: 'Gather' }, { title: 'Dedup' }, { title: 'Triage' }, { title: 'Verify' }],
}

const STAGING = (args && args.staging_branch) || 'homolog'
const GUARDS = (args && args.guards) || []

const guardBlock = GUARDS.length
  ? `\nPROJECT GUARD RULES — apply every one; if a rule directs a verdict, that verdict wins:\n${GUARDS.map((g, i) => `  ${i + 1}. ${g}`).join('\n')}\n`
  : `\n(No project guard rules supplied. Still treat any change to a cross-system contract or a shared multi-platform path as human-only-integration by default.)\n`

const RULES = `HARD RULES (READ-ONLY triage agent):
- READ-ONLY: never close/comment issues, never append ledger events, never edit/commit/branch. Output is the structured verdict only; the caller applies all side-effects.
- cwd = the repo root. Establish current truth on the "${STAGING}" branch HEAD.
- Confirm from CODE, not one source. Cross-check >=2 of: real code (Read/Grep), git history (git log -S'<token>' / --grep), framework runtime probes (Laravel: php artisan db:table <t> to prove a table/column exists, php artisan tinker --execute='var_export(DB::select("…"));' LOCAL-only for data — prefer artisan over the laravel-boost MCP, which costs far more tokens) and graphify if a .graphify graph is present.
- graphify is a HINT, not ground truth. A graph exists if \`graphify-out/graph.json\` OR \`.graphify\` is
  present; query it from the repo root with no flags: graphify query "is <behavior> implemented, and
  where" / graphify explain "<Symbol>". Use it to LOCATE the code for an item fast, never as the
  close evidence itself — artifact_ref must be a real file:line / SHA / PR you read. ALWAYS confirm
  against real code + git. If no graph, skip it.
- FORBIDDEN: never call a long-poll / remote-docs MCP that can block (it wedges a fan-out agent for hours with no error). Use code + git + a local framework MCP only.
${guardBlock}- FOUR DIMENSIONS — code alone is the known failure mode; it misses items whose only evidence lives
  in a completed plan or on an in-flight branch. Check all four before any close:
    1. CODE on ${STAGING} HEAD.
    2. ACTIVE WORKTREES — \`git worktree list\`; if a branch matches this item's feature, diff it vs
       ${STAGING}. Ignore \`.claude/worktrees/*\` (agent scratch). A match means verdict 'in-flight'.
    3. OPEN PRs — \`gh pr list --state open\`. Work can be in-flight and undocumented. A match means
       verdict 'in-flight'. NEVER close an item that an open PR is currently implementing.
    4. .planning/ PLAN FOLDERS — THE ONE MOST EASILY MISSED. Plans NEVER contain backlog ids (items
       are filed AFTER plans), so MATCH BY FEATURE/BEHAVIOR, NOT by grepping the id. A
       "-complete"/"-completed" suffix means SHIPPED -> that folder IS a valid artifact_ref. But a
       completed plan that explicitly DEFERS scope "to a separate backlog item" VALIDATES keeping
       this item open — read the plan before citing it.
  Also check recent merge batches: \`git log origin/main --oneline --since="14 days ago" --merges\`.
  A single integration PR can land many fixes at once and silently resolve several items.
- LEDGER DEFECTS (backlog items only): set ledger_defect when the id's metadata lies. Two patterns:
  duplicate-id twins (\`ls .planning/backlog/ | grep -E '<id>'\` returns 2+ files — any close targets
  an ambiguous subject), and (legacy items) a frontmatter \`status:\` that disagrees with the FILENAME
  suffix (the filename is canonical for legacy items; events in .planning/events.jsonl outrank both).
  A ledger defect is reported for a human, never auto-closed on.
- CLOSE EVIDENCE BAR: already-done / solved-differently REQUIRES a concrete artifact_ref you READ — a commit SHA, merged PR #, plan id, or precise file:line. No verified ref -> downgrade to genuinely-open or already-done-partial (name what is still missing). Never invent a SHA.`

const GATHER_SCHEMA = { type:'object', additionalProperties:true, required:['issues','backlog'], properties:{
  issues:{type:'array', items:{type:'object', additionalProperties:true, required:['id','title'], properties:{ id:{type:'string'}, title:{type:'string'} }}},
  backlog:{type:'array', items:{type:'object', additionalProperties:true, required:['id','title','path'], properties:{ id:{type:'string'}, title:{type:'string'}, path:{type:'string'} }}},
} }

const TRIAGE_SCHEMA = { type:'object', additionalProperties:true,
  required:['source','kind','verdict','artifact_ref','touches_contract','summary','detail'],
  properties:{
    source:{type:'string'}, kind:{type:'string', enum:['issue','backlog']},
    verdict:{type:'string', enum:['already-done','already-done-partial','solved-differently','no-longer-needed','genuinely-open','in-flight','human-only-integration','too-big']},
    artifact_ref:{type:'string'}, touches_contract:{type:'boolean'},
    confidence:{type:'string', enum:['high','medium','low']},
    ledger_defect:{type:'string', description:'duplicate-id twin or frontmatter/filename desync for this id, else empty string'},
    in_flight_ref:{type:'string', description:'worktree branch or open PR # building this right now, else empty string'},
    summary:{type:'string'}, detail:{type:'string'},
  } }
const VERIFY_SCHEMA = { type:'object', additionalProperties:true, required:['source','confirmed'],
  properties:{ source:{type:'string'}, confirmed:{type:'boolean'}, reason:{type:'string'} } }
const DEDUP_SCHEMA = { type:'object', additionalProperties:true, required:['clusters'], properties:{
  clusters:{type:'array', items:{type:'object', additionalProperties:true, required:['canonical','dups'], properties:{
    canonical:{type:'string'}, dups:{type:'array', items:{type:'string'}}, reason:{type:'string'} }}},
  kept:{type:'number'}, collapsed:{type:'number'} } }

// One-shot examples — echoed verbatim into each paired prompt so agents match the shape first try.
const GATHER_EXAMPLE = { issues:[{ id:'123', title:'Login 500 on empty email' }], backlog:[{ id:'B-0007', title:'Add API rate limiting', path:'.planning/backlog/B-0007-add-api-rate-limiting.md' }] }
const TRIAGE_EXAMPLE = { source:'B-0007', kind:'backlog', verdict:'already-done', artifact_ref:'app/Http/Middleware/RateLimit.php:12 (PR #88)', touches_contract:false, confidence:'high', ledger_defect:'', in_flight_ref:'', summary:'Rate limiting shipped in PR #88', detail:'Middleware registered for all API routes; fully covers the backlog ask.' }
const VERIFY_EXAMPLE = { source:'B-0007', confirmed:true, reason:'PR #88 diff fully implements the ask on current HEAD.' }
const DEDUP_EXAMPLE = { clusters:[{ canonical:'123', dups:['B-0007'], reason:'same rate-limit ask' }], kept:41, collapsed:1 }

phase('Gather')
let issues = (args && args.issues) || null
let backlog = (args && args.backlog) || null
if (!issues || !backlog) {
  const g = await agent(`Read-only. Build the open work set for this repo:
- issues: run \`gh issue list --state open --limit 300 --json number,title\` -> [{id:"<number>", title}].
- backlog: list \`.planning/backlog/B-*.md\` -> for each ACTIVE item return {id, title, path}. An item is ACTIVE unless (a) .planning/events.jsonl (and events-*.jsonl shards) holds a terminal event (kind completed|cancelled) for its id — events win — or (b) legacy: its FILENAME carries a -complete/-cancelled suffix. Title from frontmatter. If the project has no backlog dir, return backlog: [].
Return {issues, backlog}.

Return ONLY JSON matching the schema above. Example: ${JSON.stringify(GATHER_EXAMPLE)}`, { schema: GATHER_SCHEMA, label:'gather', phase:'Gather', effort:'low', agentType:'general-purpose' })
  issues = (g && g.issues) || []
  backlog = (g && g.backlog) || []
}
const all = [...issues.map(i=>({...i, kind:'issue'})), ...backlog.map(b=>({...b, kind:'backlog'}))]
log(`triage-sweep: ${issues.length} issues + ${backlog.length} backlog = ${all.length} items`)
if (!all.length) return { dedup:{clusters:[],kept:0,collapsed:0}, results:[] }

phase('Dedup')
const dedup = await agent(`${RULES}

DEDUP TASK. Below is the full open work set as {id,title}. Cluster CLEAR duplicates ONLY (issue<->issue same root error/endpoint+symptom; backlog<->backlog same ask; issue<->backlog same problem). You MAY \`gh issue view <n>\` / read a backlog file to confirm. Be CONSERVATIVE — collapse only when confident it is the same underlying fix; leave unsure pairs OUT. For each cluster>1 pick the CANONICAL to keep (richer GH issue; else lowest backlog id) and list the others in dups.

WORK SET:
${JSON.stringify({issues:all.filter(x=>x.kind==='issue').map(x=>({id:x.id,title:x.title})), backlog:all.filter(x=>x.kind==='backlog').map(x=>({id:x.id,title:x.title}))})}

Return ONLY JSON matching the schema above. Example: ${JSON.stringify(DEDUP_EXAMPLE)}`,
  { schema: DEDUP_SCHEMA, label:'dedup', phase:'Dedup', effort:'medium', agentType:'general-purpose' })
const dupIds = new Set((dedup?.clusters||[]).flatMap(c=>c.dups||[]))
const triageItems = all.filter(it => !dupIds.has(it.id))
log(`dedup: collapsed ${dupIds.size}, triaging ${triageItems.length}`)

phase('Triage')
const triagePrompt = (it) => `${RULES}

TRIAGE ONE ITEM — ${it.kind} ${it.id}: "${it.title}".
${it.kind==='issue' ? `Read it + comments: \`gh issue view ${it.id} --comments\`.` : `Read the backlog file: ${it.path}`}
Establish current truth on ${STAGING} HEAD across ALL FOUR DIMENSIONS above (code + worktrees + open PRs + .planning plan folders; real code + git + a framework MCP if present; graphify only a hint) and classify into EXACTLY ONE: already-done | already-done-partial | solved-differently | no-longer-needed | genuinely-open | in-flight | human-only-integration | too-big. already-done/solved-differently MUST cite an artifact_ref you read — a completed .planning plan folder counts. in-flight MUST cite the branch or PR in in_flight_ref. For a backlog item, also set ledger_defect if its id has a twin file or a frontmatter/filename desync. Be SKEPTICAL — prove already-done, do not rubber-stamp, do not invent a SHA. Return the struct only.

Return ONLY JSON matching the schema above. Example: ${JSON.stringify(TRIAGE_EXAMPLE)}`

// pipeline() await-contract harness-validated 2026-06-21 — smoke `pipeline([1,2,3], x=>x*2, (y,orig)=>y+orig)`
// returned [3,6,9], confirming each stage's return (value OR Promise) is awaited and stage2 receives
// (prevResult, originalItem). F10: every close is evidence-gated to a FIXED artifact_ref the triage agent READ,
// so any HEAD drift between this parallel classify and the caller's later SERIAL apply is benign — a stale ref
// just fails the adversarial verify and the close is skipped, never wrongly applied.
const results = await pipeline(
  triageItems,
  (it) => agent(triagePrompt(it), { schema: TRIAGE_SCHEMA, label:`triage:${it.id}`, phase:'Triage', effort:'high', agentType:'general-purpose' }),
  (v, it) => {
    if (!v) return null
    const base = { ...v, kind: it.kind, title: it.title, path: it.path||null }
    // ledger_defect blocks auto-close: with a duplicate-id twin, a close event names an
    // ambiguous subject, so the defect must be resolved by a human first.
    const eligible = (v.verdict==='already-done' || v.verdict==='solved-differently') && v.artifact_ref && !v.touches_contract && !v.ledger_defect
    if (!eligible) return { ...base, verification: null }
    return agent(`${RULES}

ADVERSARIAL VERIFY — ${it.kind} ${it.id}: "${it.title}". A triage agent concluded **${v.verdict}** -> CLOSE, citing: ${v.artifact_ref}. Summary: ${v.summary}. Detail: ${v.detail}
REFUTE the close. Open the cited artifact (git show / gh pr view / read file:line) AND inspect current ${STAGING} code (a framework MCP for schema/route facts; not graphify alone). Default confirmed=false unless the ref genuinely + FULLY resolves THIS item on current HEAD. confirmed=true only if airtight.

Return ONLY JSON matching the schema above. Example: ${JSON.stringify(VERIFY_EXAMPLE)}`,
      { schema: VERIFY_SCHEMA, label:`verify:${it.id}`, phase:'Verify', effort:'high', agentType:'general-purpose' }
    ).then(ver => ({ ...base, verification: ver }))
  }
)
return { dedup: dedup || {clusters:[],kept:0,collapsed:0}, results: results.filter(Boolean) }
