// Reusable named workflow — generate/audit CLAUDE.md files, one `claudemd-author` agent per
// directory cluster, in parallel. The CALLER (the /context skill) presents the results for review
// and lets the user decide what to keep — this workflow does NOT commit.
//   Workflow({ name: 'context-authors', args: { stack_json: '{...}', clusters: [
//     { name: 'app', dirs: ['app/Models','app/Http'], template: '<framework dir template>', docs: '<context7 notes>' },
//   ] } })
//
// MUTATING (CLAUDE.md ONLY): the agent is the `claudemd-author` persona (tools: Read, Grep, Glob,
//   Edit, Write, Bash) — it edits ONLY CLAUDE.md files, at most one per directory, never application
//   code or configs, and never commits. One cluster per agent; siblings own disjoint dirs.
//
// args.clusters : [{ name, dirs:[...], template?, docs? }] — one fan-out worker per cluster.
// args.stack_json : the detected stack JSON string, injected into every prompt for version-correct
//   guidance.

export const meta = {
  name: 'context-authors',
  description: 'Parallel CLAUDE.md authoring: one claudemd-author per directory cluster generates or audits that cluster\'s CLAUDE.md (CLAUDE.md files only, never commits) and returns a schema-validated rollup.',
  whenToUse: 'When /context needs to generate or audit a tree of CLAUDE.md files across many directory clusters at once and collect what each worker wrote/audited as structured data for the user to review.',
  phases: [{ title: 'Author', detail: 'one claudemd-author agent per directory cluster (parallel) -> per-cluster files_written / files_audited / staleness' }],
}

const CLUSTERS = (args && args.clusters) || []
const STACK_JSON = (args && args.stack_json) || '{}'

const CLUSTER_SCHEMA = {
  type: 'object',
  additionalProperties: true,
  required: ['cluster'],
  properties: {
    cluster: { type: 'string' },
    files_written: { type: 'array', items: { type: 'string' } },
    files_audited: { type: 'array', items: { type: 'string' } },
    staleness: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: true,
        required: ['file', 'issue'],
        properties: { file: { type: 'string' }, issue: { type: 'string' } },
      },
    },
  },
}

// One-shot example echoed into every schema'd prompt — the #1 schema-failure fix: show the exact
// shape once so workers return it verbatim instead of inventing wrappers.
const ONE_SHOT_CLUSTER = `Return ONLY JSON. Example: {"cluster":"app","files_written":["app/Models/CLAUDE.md"],"files_audited":["app/Http/CLAUDE.md"],"staleness":[{"file":"app/Http/CLAUDE.md","issue":"referenced removed middleware alias"}]}`

const clusterPrompt = (c) => `You are the claudemd-author. Generate or audit the CLAUDE.md for THIS directory cluster ONLY — never the whole tree. Working dir is the repo root.

CLUSTER: ${c.name}
DIRECTORIES (touch ONLY these — a sibling worker owns every other cluster):
${(c.dirs || []).map(d => '  - ' + d).join('\n')}

DETECTED STACK (version-correct guidance — use it, do not re-detect):
${STACK_JSON}
${c.template ? '\nFRAMEWORK DIRECTORY TEMPLATE (apply only to dirs that exist + patterns the code confirms):\n' + c.template + '\n' : ''}${c.docs ? '\nCONTEXT7 DOCS:\n' + c.docs + '\n' : ''}
METHOD:
1. For each dir, decide mode: CLAUDE.md exists -> audit; absent + dir warrants one (5+ files with shared patterns, non-obvious rules, complex flows, gotchas, external API integrations) -> generate; thin/obvious dir -> skip.
2. Read the REAL code first — never guess patterns. Read representative files (models, controllers, services, components, configs) to confirm the conventions actually in use.
3. Generate -> a LEAN CLAUDE.md. Two-question test on every line before it goes in: (a) would the model have known/inferred this from the code on its own? -> leave it out; (b) is it something only THIS repo knows — non-obvious convention, hard-won trap, tool gotcha, authority boundary? -> keep. No framework defaults restated, no directory listings the tree already shows, no generic best practices ("write tests", "follow PSR-12"). Well under 200 lines; shorter is better; a dir with nothing only-we-know gets NO file. Audit -> edit ONLY what is stale (wrong framework version, deprecated APIs, missing conventions for new files, dead path refs, wrong test patterns) and DELETE lines that fail the two-question test; leave correct prose untouched.

HARD RULES:
- Edit ONLY CLAUDE.md files — never application code, configs, or any other file. At most one CLAUDE.md per directory.
- Never run the full test suite — targeted reads + git/grep/find only.
- Do NOT commit — the caller lets the user review generated files.

Return ONLY the structured object:
- cluster: "${c.name}"
- files_written: relative paths of CLAUDE.md files you GENERATED (new)
- files_audited: relative paths of existing CLAUDE.md files you reviewed (edited or confirmed current)
- staleness: for each stale item you found/fixed, { file, issue (what was stale) }.
${ONE_SHOT_CLUSTER}`

phase('Author')

const results = await parallel(CLUSTERS.map(c => () =>
  agent(clusterPrompt(c), { schema: CLUSTER_SCHEMA, label: `context:${c.name}`, phase: 'Author', model: 'sonnet', effort: 'high', agentType: 'claudemd-author' })
))

return { clusters: CLUSTERS.map(c => c.name), results: results.filter(Boolean) }
