---
description: Close a finished task - write its note, update the docs it made stale, update PROGRESS.md
argument-hint: "[task ID, default: the task just ticked]"
---

Run after a task is ticked in docs/PLAN.md (work loop step 7). A hook reminds you when a tick lands. Task: $ARGUMENTS, or the one just ticked. Work outside PLAN.md (a fix, a redesign) uses a dated slug as its ID, for example `2026-09-23-welcome-page`, and skips step 2.

1. **Note.** Write docs/notes/<ID>.md for a teammate who did not watch the work. Facts only, from this session and fresh command output:

   ```markdown
   # <ID> <task title>

   Date: <YYYY-MM-DD> · Requirements: <IDs> · Agent: <be / fe / ops>

   ## What was done
   2–5 bullets in plain words: what a guest, agent or admin can now do, and how the code does it.

   ## Files
   Each created or changed file, one line with its purpose.

   ## Decisions
   Choices made without asking a human, and how to reverse each. "None." if none.

   ## Checks
   Commands run and their result: test names, pass counts, e2e, security review outcome.

   ## Docs updated
   Each doc changed in step 3 and what changed. "None." if none.

   ## Follow-ups
   Open points for later tasks. "None." if none.
   ```

2. **PLAN.md.** Replace the task's Status line with one sentence and a link: `Status: done. <one sentence>. See [note](notes/<ID>.md).`
3. **Docs.** Update only a section this task made wrong or incomplete. Leave the rest alone.
   - docs/ARCHITECTURE.md: new package, background job, service, table or column, deploy piece.
   - DESIGN.md: new component or page layout spec, if the fe agent did not already add it.
   - README.md, CLAUDE.md, .env.example: new command, tool, env var (placeholder value only), dev account or workflow step.
   - docs/REQUIREMENTS.md: never change a requirement. If the build differs from one, add a question for a human under Blockers in PROGRESS.md.
4. **PROGRESS.md.**
   - Recount done / total per phase from the checkboxes in docs/PLAN.md.
   - Set "Current focus" to the next unchecked task. Name any decision from step 1 that needs a human review, with a link to its note.
   - Add one log line: `- <YYYY-MM-DD> — <ID> done (<requirement IDs>): <one sentence>. [Note](docs/notes/<ID>.md)`
   - Move new blockers and resolved questions to "Blockers".

Keep PROGRESS.md short. Never rewrite older log lines or older notes.
