---
description: Pick the next unchecked task from docs/PLAN.md and build it
argument-hint: "[optional task text to pick instead]"
---

Run one pass of the "Work loop" in CLAUDE.md, steps 1–8, for one task.

- Pick the first unchecked task in the current phase (see PROGRESS.md), or the task matching: $ARGUMENTS
- Build backend work through the go-backend agent, UI work through the svelte-frontend agent.
- Stop after one task. Do not start the next one.
