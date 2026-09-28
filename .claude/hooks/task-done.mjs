// PostToolUse hook (Edit): when an edit ticks a task in docs/PLAN.md, remind Claude to close it with /progress.
let raw = '';
for await (const chunk of process.stdin) raw += chunk;
const input = JSON.parse(raw || '{}').tool_input ?? {};
if (!/docs[\\/]PLAN\.md$/.test(input.file_path ?? '')) process.exit(0);

const ticked = (s) => new Set([...(s ?? '').matchAll(/- \[x\] \*\*((?:T\d+|P)\.\d+)\*\*/g)].map((m) => m[1]));
const before = ticked(input.old_string);
const now = [...ticked(input.new_string)].filter((id) => !before.has(id));
if (now.length === 0) process.exit(0);

console.log(JSON.stringify({
  hookSpecificOutput: {
    hookEventName: 'PostToolUse',
    additionalContext: `Task ${now.join(', ')} ticked in docs/PLAN.md. Run /progress ${now.join(' ')} now: it writes docs/notes/<ID>.md, syncs the docs this task made stale and updates PROGRESS.md.`,
  },
}));
