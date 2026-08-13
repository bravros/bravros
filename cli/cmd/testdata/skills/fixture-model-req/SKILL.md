---
name: fixture-model-req
description: >
  A test fixture skill for verifying Claude model reference stripping in converted output.
  Triggers on "/fixture-model-req" or "run fixture".
targets:
  - claude
---

## Model Requirement

This skill requires **Sonnet** (claude-sonnet-4-6) for best results.
Opus is overkill; Haiku lacks sufficient reasoning for this task.

# Fixture Skill

This skill demonstrates Claude Sonnet capabilities. For heavy work, Claude Opus 4.6 is available.
Use Claude Haiku for lightweight tasks.

## Steps

1. Do the thing
2. Verify result
