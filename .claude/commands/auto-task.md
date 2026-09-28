---
description: Build the next ready task, recheck it, then continue (run as /loop /auto-task)
---

One unattended pass of the "Work loop" in CLAUDE.md. Start it with `/loop /auto-task`; each pass builds one task, and /loop fires the next pass.

1. **Pick.** The first unchecked task in docs/PLAN.md whose deps are all ticked and that is not under Blockers in PROGRESS.md. If none is left, list the open tasks with what blocks each, then end the loop.
2. **Build.** Work loop steps 1–6, as `/next-task` does. Nobody is watching: where the loop says "stop and ask", write the question under Blockers in PROGRESS.md, add "blocked: <reason>" to the task's Status line, and pick the next ready task instead.
3. **Recheck.** Before ticking, verify again from scratch, not from memory:
   - `/check` passes.
   - Each done criterion in PLAN.md is proven by fresh command output (test name and its pass line).
   - UI changed: `cd frontend && npm run test:e2e` passes (stop a Go API left running from before a backend change first).
   - Change touches auth, RBAC, audit, guest endpoints or uploads: security-reviewer has no open findings.
   - Changed files hold no TODO, FIXME, skipped test or English-only key missing from the other message files.

   A miss: fix it and recheck. The same cause fails 3 times: record it under Blockers, leave the task unticked, go on.
4. **Close.** Tick the task in docs/PLAN.md, run `/progress`, do step 8 (Learn).
5. **Continue.** End the pass with one line: task, result, next ready task.

End the loop instead of continuing when `/check` fails for a cause outside the current task, or when a blocked task leaves the tree broken for the next one.
