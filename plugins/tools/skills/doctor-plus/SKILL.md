---
name: doctor-plus
description: Use on /doctor-plus or "context checkup" to audit workspace context (guidance files, skills, rules, memory) against Anthropic's 6 context-engineering shifts. Reports first; fixes only on approval. Built-in /doctor is separate.
---

# Doctor Plus

Audits workspace context against Anthropic's 6 context-engineering shifts. The built-in `/doctor` health check is separate — point the user to it; never run `claude doctor` from Bash.

Read [briefing.md](references/briefing.md) on demand for detailed context and instructions.

## Key Workflow Summary

1. **Built-in checkup**: do not run `claude doctor`; tell the user to run the built-in `/doctor` themselves, then continue.
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
