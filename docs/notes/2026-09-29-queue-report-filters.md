# 2026-09-29-queue-report-filters Every report card opens exactly its tickets

Date: 2026-09-29 · Requirements: FR-P1, FR-P2 · Agent: be + fe (main agent)

## What was done
- `GET /api/staff/tickets` takes the report's filters: `building` (English name), `category_id`, a period (`by=created|resolved`, `from`, `to`, days in `tz`) and `as_of`. With `as_of`, status, priority and assignee are the ones a ticket had at the end of that day, rebuilt from audit_log exactly as the report's open counts are.
- Each summary card on /staff/reports opens the queue with its own tickets, keeping the report's building, category and time zone, so the queue total equals the card:
  - Open, Unassigned and Urgent open: the open statuses as of the period end;
  - New and Resolved: the period on created or resolved time;
  - the two medians: the tickets each is measured over (the New list or the Resolved list).
- The queue page has Building and Category selects. The period and as-of filters, which only a card sets, show as removable chips ("Created: 1 Sep 2026 – 30 Sep 2026", "Status as at the end of …").
- This answers the T3.03 question; the approximation in the T3.03 note no longer applies.

## Files
- backend/internal/api/queue.go: the new filters, `queueBounds` (dates to instants in tz, and the tz check, in one query) and the as-of derived table; `stateFilters` split out.
- backend/internal/api/queue_test.go: `TestQueueMatchesReportCards_FRP1` and `TestQueue_BadReportFilters_FRP1`.
- frontend/src/lib/api.ts: `getBuildingOptions` (shared by reports and queue).
- frontend/src/routes/staff/+page.ts, +page.svelte: the new filters and chips.
- frontend/src/routes/staff/reports/+page.ts, +page.svelte: `getBuildingOptions`; the card scope.
- frontend/src/lib/components/ReportCards.svelte: the exact links.
- frontend/e2e/reports.spec.ts: "each card opens the queue with exactly its tickets" replaces the Open-card test.
- frontend/messages/{en,zh-CN,my,th}.json: 4 chip keys (AI translations, to review with the rest, T3.07).
- DESIGN.md: see Docs updated.

## Decisions
- `as_of` rather than a separate "open at" filter: the same status, priority and assignee filters apply to the rebuilt state, so the Open, Unassigned and Urgent links differ only in the usual parameters.
- The queue still shows each ticket's status now; the chip says what the list was filtered on.
- Security review (medium): "`as_of` exposes audit history to roles without audit.view". No change. The staff ticket detail already shows every `ticket.view_all` holder each ticket's timeline, built from the same audit_log rows (FR-L3, detail.go), so `as_of` reveals nothing new. The reason is written next to `asOfState` in queue.go.
- Security review (low): the as-of subqueries run for the count and again for the page. Accepted for now, with a `ponytail:` note naming the upgrade.
- No date inputs on the queue page: the period and as-of come from a card and can be removed, not typed. Add date inputs if staff ask for them.

## Checks
- Test first: `TestQueueMatchesReportCards_FRP1` failed before the change. The queue ignored the new parameters, so totals were 784 and 896 against cards of 4 and 5.
- After the change, all 6 queue tests pass, including the 3 scopes of the new test (building, category, and a one-day Bangkok period). `make test-go PKG=./...`: all packages ok.
- golangci-lint: 0 issues; svelte-check: 0 errors; `check-i18n`: exit 0.
- e2e against the built app on :8090 (with the helpers' axe fix from T3.17):
  - `reports.spec.ts` and `staff-queue.spec.ts`: 5/5 pass, including "each card opens the queue with exactly its tickets" (7 cards, 5 totals compared, the chip removed) and axe on the queue page with the new selects.
  - `live.spec.ts`: passes. An earlier run failed while the machine was overloaded (a 1.2 s staff lookup).
  - `layout.spec.ts` "queue summary is readable at tablet widths": the summary cell is 174.7–180.3px, under the 200px minimum, in Burmese at 1024px, on rows showing "not assigned yet". A probe with an assigned first row measured 225px. The table markup is unchanged here (the new selects sit above it), so this is recorded as a separate, data-dependent layout issue (Follow-ups).
- Security review: see Decisions.

## Docs updated
- DESIGN.md: "Staff queue" lists the building and category selects and specs the chips; "Reports dashboard" describes the exact card links in place of the old limits.

## Follow-ups
- Queue table at 1024px in Burmese: with "not assigned yet" and the long Burmese date, the summary column drops below 200px. Consider a narrower date format or moving the table breakpoint.
