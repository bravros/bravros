# Interview Me — Detailed Briefing

Closes the gap between "Claude thinks it understands" and "Claude actually understands" — for plans that look complete but hide unresolved decisions three layers deep. Most common entry point: after `/plan`, to lock down decisions the planner punted on. Standalone mode (no plan file) works the same against whatever is in the conversation.

Map the plan as a **design tree**: every decision branches into the decisions that hang off it. Work the tree in **rounds** over the **frontier** — every unresolved decision whose prerequisites are already settled, i.e. the questions you can ask *now* without guessing at answers you haven't heard yet.

## Critical Rules

These five gates ARE the skill. Breaking any one is a failure, not a style choice.

1. **Ask by rounds — the whole frontier per round, one `ask_question` call.** Batch only *independent* questions: a question whose answer depends on another question still open in this round belongs to a *later* round, never this one. `ask_question` takes up to 4 questions per call; a frontier larger than 4 keeps its most upstream 4 and rolls the rest forward (they're still frontier next round). The upfront bulleted list (core loop step 3) is a *preview so the user can catch what you missed* — it is NOT the questions.
2. **Every question carries a recommendation.** Place your recommended option **first**, append **`(Recommended)`** to its label, and give the one-line why in the framing (*"I'd pick X because …, but —"*). A question with no recommendation dumps the decision back on the user instead of giving them something to say "yes" to.
3. **Recompute the frontier after every round.** Each round of answers reshapes the tree — settled decisions push the frontier outward, eliminate questions, add new ones, or reword what's ahead. Never march through a fixed list.
4. **No round cap.** The session is done when the frontier is empty — every branch visited, nothing left silently assumed — or the user says "stop / enough / we're done". Never self-limit to a round or two and wrap up early.
5. **Facts are your job, never the user's — decisions are theirs, always.** Resolve every *structural* question yourself: `graphify query "<question>"` to find it, **then read the file it points at to confirm** — graph labels go stale and a wrong one reads like a right one. Ask the user ONLY about intent and preference, and never answer a *decision* on their behalf, however obvious it looks.

## The core loop

1. **Read the source material** fully — the plan file if one is in scope, otherwise the conversation.
2. **Build a provisional design tree**: every branch point where the choice isn't locked, an assumption went unconfirmed, or two reasonable engineers would differ. Order by dependency.
3. **Show the user only the unresolved branches** — short bulleted list, one line each: *"Here's what I want to lock down — flag anything missing or already-decided."*
4. **Ask round by round**: compute the frontier, resolve its fact-questions yourself (below), put its decision-questions to the user in one `ask_question` call, then recompute from the answers.
5. **Capture decisions as they lock** (see "Docs as you go"), and when the frontier is empty, close out (below).

## Asking questions

Default to `ask_question` with 2–4 short, mutually exclusive options per question; question text is one sentence, no preamble. Free-text is the right call when options would be artificial ("paste the error message", "what's the actual table name").

**The `preview` field** renders a side-by-side comparison for code snippets or mockups — use it when the decision hinges on seeing two alternatives together. Gotchas: `preview` is single-select only, and the side-by-side layout works best alone — give a preview question its own single-question call rather than burying it in a full round; the harness auto-injects an "Other" free-type option (never add it yourself); `header` text over 12 characters silently truncates; `multiSelect: true` returns option values as arrays.

## Check code before asking

Classify each candidate question:

- **User's intent or preference** ("void labels or skip them?") → ask the user. Only this category reaches `ask_question`.
- **How the code currently behaves** → check graphify/code, resolve silently, drop the question.
- **Unsure which** → look at the code first; if it doesn't settle it, it was an intent question — ask.

### Graphify first, then confirm in the code

**Query graphify first whenever the project has it** — a `.graphify` file or `graphify-out/graph.json`. It resolves "how does X work / what touches Y / who calls Z" in one call instead of a grep sweep, which is the difference between resolving a question silently and burning the user's patience mid-interview.

```
mcp_graphify__query_graph {question: "<question>"}    — one call, graph stays warm across rounds
mcp_graphify__shortest_path {source, target}          — fuzzy labels; "how does A reach B"
graphify query "<question>"                            — CLI backup (also what a dispatched agent uses)
```

**Then open the code it points you at, before you treat the answer as settled.** The graph is a map, not the territory:

- Community labels are keyed by cluster id, and any rebuild that re-clusters renumbers them — so a confidently-named cluster can describe code that has since moved or been deleted. A wrong label reads exactly like a right one.
- Graphify answers *code structure* only. Runtime behavior, data shape, and anything in the database it cannot see — those need the actual file, a query, or a test.

So the loop is **graph → locate → read the file → then decide whether the question is dead.** Dropping a question on the graph's word alone is how an interview silently locks in a wrong premise; the whole point of this skill is to stop that happening. If the graph and the code disagree, the code wins and the graph is stale — say so.

**No graphify in this project?** Same rule, one step shorter: grep/read to resolve it, and only ask if the code genuinely doesn't settle it.

### Slow lookups never block the round

A quick graph query + file read: do it inline — announce it (*"Hold on, let me check how X works"*) and come back with what you found; never silently disappear. A lookup that needs real digging (multi-file trace, running something): dispatch a background subagent (`Explore`/`scout`) and **ask the rest of the frontier now** — a running exploration is just an unsettled prerequisite, so only the questions downstream of it wait for the report. Research runs while the user answers; nobody idles. Tell that subagent to start from the graph — `graphify query "<question>"` from the repo root, then read the files it names — since it has Bash but not the MCP server; a multi-file trace is the exact shape the graph collapses into one call.

## Docs as you go

Capture locked decisions **as each round closes**, not batched at the end — a crashed session should lose at most one round.

- **Folder plan in scope** (`.planning/P-NNNN-<slug>/`): append to `decisions.md` inside the plan folder, using the output format below.
- **Single-file plan or standalone**: accumulate in the conversation, and at close write `.planning/decisions/<topic-slug>.md` following that directory's convention — frontmatter (`title`, `date`, `status: locked`, `scope`), a short context paragraph so the file reads on its own, then the decision list. No `.planning/` in the project? Offer a location and let the user override.

If a decision contradicts something already written in the plan or an earlier decisions file, flag the conflict in the same round — don't silently record both.

## Closing the interview

**Plan file in scope** (most common): decisions ripple through any section — don't bolt on a Q&A appendix. Either **re-run `/plan`** with the locked decisions in context (if the interview shifted the plan's overall shape), or **apply targeted edits** to the sections that changed (if only a few details were refined). Tell the user which path you're taking before doing it.

**Standalone mode**: finalize the `.planning/decisions/<topic-slug>.md` file (above) and report the path.

Do not act on the plan until the user confirms the shared understanding — the empty frontier ends the *interview*, the user's confirmation ends the *session*.

## Output format for locked decisions

```markdown
## Locked decisions (interview YYYY-MM-DD)

1. **<Decision in one phrase>.** <One-sentence rationale — why this choice over the alternatives.>
2. **<Next decision>.** <Rationale.>
```

Bold the decision so it's scannable; one-sentence rationales — this list is the durable summary, not the transcript.

## Tone

Like pairing with a thoughtful colleague, not an interrogation. Short framing sentences, recommendations stated plainly, no over-justification — respect the user's time by keeping each round tight.
