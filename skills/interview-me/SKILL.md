---
name: interview-me
description: Stress-test a plan or design via a round-by-round interview — ask the whole frontier of independent questions each round until every branch of the decision tree is locked. Use on `/interview-me`, "interview me", or when the user signals doubt about a plan.
---

# Interview Me

Read [briefing.md](file:///Users/skaisser/Sites/bravros/skills/interview-me/references/briefing.md) on demand for detailed context and instructions.

Stress-test plans or designs by mapping unresolved choices into a **design tree** and interviewing the user round-by-round over the decision **frontier**.

## Critical Rules

1. **Ask by rounds (max 4 per `ask_question`)**: Only batch independent questions on the current frontier.
2. **Always include a recommendation**: Place recommended option first, append `(Recommended)`, and explain why in 1 sentence.
3. **Recompute frontier every round**: Do not use a static question list; answers alter downstream branches.
4. **No round limit**: Continue until the decision frontier is completely empty or user says stop.
5. **Facts are your job, decisions are theirs**: Resolve structural code questions silently via graphify/code before asking.

## Core Workflow

1. **Map Tree**: Identify unresolved branches in the plan/conversation.
2. **Preview**: Show short bulleted list of open branches to the user.
3. **Interview Loop**:
   - Check code/graphify first for factual questions.
   - Present decision frontier via `ask_question` (with recommendations).
   - Recompute frontier after responses.
   - Record locked decisions incrementally in `.planning/decisions/` or `decisions.md`.
4. **Finalize**: Update plan file or decision log upon complete frontier exhaustion and user confirmation.
