---
name: doctor-plus
description: Runs Claude Code's built-in /doctor health check, then audits the workspace against Anthropic's 6 then-and-now context-engineering shifts (rules→judgement, examples→interfaces, progressive disclosure, one-home instructions, auto-memory, rich references). Reports first, fixes only on approval. Use on /doctor-plus or "context checkup".
---

# Doctor Plus

Runs standard `claude doctor` health check and audits workspace context against Anthropic's 6 context-engineering shifts.

Read [briefing.md](file:///Users/skaisser/Sites/bravros/skills/doctor-plus/references/briefing.md) on demand for detailed context and instructions.

## Key Workflow Summary

1. **Standard Checkup**: Run `claude doctor` (60s timeout). Summarize findings or note manual execution if interactive.
2. **Context Shift Audit**: Audit guidance files, skills, rules, and memory indexes against 6 shifts:
   - Judgement over rules
   - Interfaces over examples
   - Progressive disclosure over upfront loading
   - One home over repetition
   - Auto-memory over guidance-file memory
   - Rich references over simple specs
3. **Report & Wait**: Output findings table (shift, verdict, worst offender, suggested fix). **Show everything first, change nothing until approved.**

## Core Rules
- Cite exact files and passages breaking principles.
- Respect intentional hard rules (approvals, backups, safety, financial).
