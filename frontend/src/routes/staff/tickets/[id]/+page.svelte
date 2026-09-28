<!-- Staff ticket detail (T1.19) with actions, reply box and timeline (T2.06, FR-L3). Layout: DESIGN.md "Ticket detail". -->
<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { invalidate, invalidateAll } from '$app/navigation';
	import {
		addStaffComment,
		ApiError,
		assignTicket,
		staffFileURL,
		updateTicket,
		type StaffTicket,
		type TicketPatch,
		type TimelineEvent
	} from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import Badge, { labels, priorities, type Status } from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import Select from '$lib/components/Select.svelte';
	import TextArea from '$lib/components/TextArea.svelte';
	import { showToast } from '$lib/components/toast-state.svelte';
	import { languageNames } from '$lib/LanguageSwitcher.svelte';
	import { coalesce, onTicketEvent } from '$lib/live.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale, type Locale } from '$lib/paraglide/runtime';

	let { data } = $props();
	const t = $derived(data.ticket);

	// Display only: the API checks each permission again (FR-R3).
	const can = (p: string) => data.me.permissions.includes(p);
	const selfAssignOnly = $derived(data.me.role === 'Agent'); // the API lets an Agent assign only themselves

	// Mirrors staffTransitions in backend/internal/api/actions.go. Nothing moves to or from closed.
	const moves: Partial<Record<Status, Status[]>> = {
		new: ['in_progress'],
		in_progress: ['waiting', 'resolved'],
		waiting: ['in_progress'],
		resolved: ['in_progress']
	};
	// The Details form is filled from `base`: the ticket as opened, as reloaded after an own action, or as live-refreshed
	// (FR-P3) while the form held no unsaved edits. The rest of the page always shows the latest ticket, so a change
	// elsewhere never overwrites what the user is editing. Save sends only what differs from base, so it cannot revert
	// someone else's change either.
	const ticketId = $derived(t?.id); // another ticket (navigation) refills the form
	let base = $derived(ticketId === undefined ? null : untrack(() => data.ticket));
	const nextStatuses = $derived(base ? (moves[base.status] ?? []) : []);
	const option = (code: keyof typeof labels) => ({ value: code, label: labels[code]() });
	const baseCategory = $derived(String(base?.category_id ?? ''));
	const baseAssignee = $derived(String(base?.assignee?.id ?? ''));

	// Form values start from base and reset when it changes (writable deriveds).
	let status = $derived<string>(base?.status ?? '');
	let priority = $derived<string>(base?.priority ?? '');
	let categoryId = $derived(baseCategory);
	let assigneeId = $derived(baseAssignee);
	let busy = $state(false);
	let actionError = $state('');

	const dirty = $derived(
		!!base && (status !== base.status || priority !== (base.priority ?? '') || categoryId !== baseCategory || assigneeId !== baseAssignee)
	);
	// Someone else changed what the form shows while it holds unsaved edits: offer to refill it (and drop the edits).
	const formFields = (x: StaffTicket | null) => [x?.status, x?.priority, x?.category_id, x?.assignee?.id].join();
	const stale = $derived(!busy && dirty && formFields(base) !== formFields(t));
	// A change to this ticket elsewhere reloads the page; the form follows unless it holds edits. While busy, the
	// action's own reload follows anyway.
	const refresh = coalesce(async () => {
		if (busy) return;
		await invalidate('app:ticket');
		if (!dirty) base = t;
	});
	onMount(() => onTicketEvent((e) => (e.id === ticketId || e.id === 0) && refresh()));

	let replyMode = $state<'public' | 'internal'>('public');
	let reply = $state('');
	let replyError = $state<string>();

	// API error codes, translated here (CLAUDE.md "i18n").
	const errorText: Record<string, () => string> = {
		'ticket.bad_transition': m.detail_err_transition,
		'ticket.closed': m.detail_err_closed,
		'ticket.assign_self_only': m.detail_err_assign_self,
		'ticket.not_found': m.detail_not_found,
		'auth.forbidden': m.detail_err_forbidden,
		'auth.required': m.detail_err_session,
		validation: m.detail_err_invalid
	};
	const describeError = (err: unknown) =>
		err instanceof ApiError ? (errorText[err.code] ?? m.detail_err_failed)() : m.err_network();
	const bodyErrors: Record<string, () => string> = {
		required: m.err_required,
		too_long: m.err_too_long,
		invalid_characters: m.err_invalid_characters
	};

	/** Runs one action, then reloads the ticket so badges and timeline match the server, also after a conflict. */
	async function run(action: () => Promise<unknown>, done: string) {
		busy = true;
		actionError = '';
		try {
			await action();
			showToast(done);
		} catch (err) {
			actionError = describeError(err);
		}
		await invalidateAll();
		base = t; // refill with what the server holds now
		busy = false;
	}

	function save(event: SubmitEvent) {
		event.preventDefault();
		if (!base) return;
		const patch: TicketPatch = {};
		if (status !== base.status) patch.status = status as Status;
		if (priority && priority !== base.priority) patch.priority = priority as TicketPatch['priority'];
		if (categoryId && categoryId !== baseCategory) patch.category_id = Number(categoryId);
		const reassign = can('ticket.assign') && !selfAssignOnly && assigneeId !== baseAssignee;
		if (!Object.keys(patch).length && !reassign) return showToast(m.detail_no_changes());
		const id = base.id;
		run(async () => {
			if (Object.keys(patch).length) await updateTicket(id, patch);
			if (reassign) await assignTicket(id, assigneeId ? Number(assigneeId) : null);
		}, m.detail_saved());
	}

	async function send(event: SubmitEvent) {
		event.preventDefault();
		if (!t) return;
		replyError = reply.trim() ? undefined : m.err_required();
		if (replyError) return;
		const internal = replyMode === 'internal';
		busy = true;
		try {
			await addStaffComment(t.id, reply, internal);
			reply = '';
			showToast(internal ? m.detail_note_added() : m.detail_reply_sent());
			await invalidateAll();
		} catch (err) {
			const invalid = err instanceof ApiError && err.code === 'validation';
			replyError = invalid ? (bodyErrors[err.fields.body] ?? m.err_required)() : describeError(err);
			if (err instanceof ApiError && err.code === 'ticket.closed') await invalidateAll();
		} finally {
			busy = false;
		}
	}

	// Timeline (FR-L3): one translated sentence per audit action, so each language places the actor itself.
	const codeLabel = (v?: string) => (v ? (labels[v as keyof typeof labels]?.() ?? v) : m.detail_not_set());
	function describe(ev: TimelineEvent) {
		const a = ev.actor;
		const actor = a.type === 'staff' ? (a.name ?? '') : a.type === 'guest' ? m.detail_guest() : m.timeline_system();
		switch (ev.action) {
			case 'ticket.created': // from/to hold the location, never shown
				return m.timeline_created({ actor });
			case 'ticket.status_changed':
				return m.timeline_status({ actor, from: codeLabel(ev.from), to: codeLabel(ev.to) });
			case 'ticket.auto_closed':
				return m.timeline_auto_closed({ actor });
			case 'ticket.priority_changed':
				return m.timeline_priority({ actor, from: codeLabel(ev.from), to: codeLabel(ev.to) });
			case 'ticket.category_changed':
				return m.timeline_category({ actor, from: ev.from || m.detail_not_set(), to: ev.to || m.detail_not_set() });
			case 'ticket.assigned':
				return ev.to ? m.timeline_assigned({ actor, to: ev.to }) : m.timeline_unassigned({ actor });
			case 'comment.added':
				return ev.to === 'internal' ? m.timeline_note({ actor }) : m.timeline_reply({ actor });
			case 'attachment.added':
				return m.timeline_attachment({ actor });
			default:
				return m.timeline_other({ actor, action: ev.action });
		}
	}

	// Gregorian calendar in every language, including Thai (open question in docs/PLAN.md).
	const dateFmt = new Intl.DateTimeFormat(getLocale(), { dateStyle: 'medium', timeStyle: 'short', calendar: 'gregory' });
	const fmt = (iso: string | null) => (iso ? dateFmt.format(new Date(iso)) : m.detail_not_set());
	const size = (b: number) => `${(b / 1024 / 1024).toFixed(1)} MB`;
	const fileLabel = (a: StaffTicket['attachments'][number], i: number) =>
		a.media_type === 'image' ? m.track_photo({ n: String(i + 1) }) : m.track_video({ n: String(i + 1) });

	// Lightbox: a native modal <dialog> gives focus handling and Esc to close.
	let dialog: HTMLDialogElement;
	let shown = $state({ src: '', alt: '' });
	function open(src: string, alt: string) {
		shown = { src, alt };
		dialog.showModal();
	}
</script>

<a class="back" href="/staff">{m.detail_back()}</a>

{#if !t}
	<Alert variant="error">{m.detail_not_found()}</Alert>
{:else}
	<header>
		<h1>{m.detail_heading({ id: String(t.id) })}</h1>
		<Badge kind="status" value={t.status} />
		{#if t.priority}<Badge kind="priority" value={t.priority} />{/if}
	</header>

	<div class="layout">
		<section class="card details" aria-labelledby="details-title">
			<h2 id="details-title">{m.detail_details()}</h2>
			<dl>
				<dt>{m.queue_col_status()}</dt>
				<dd><Badge kind="status" value={t.status} /></dd>
				<dt>{m.queue_col_priority()}</dt>
				<dd>{#if t.priority}<Badge kind="priority" value={t.priority} />{:else}<span class="muted">{m.priority_none()}</span>{/if}</dd>
				<dt>{m.detail_f_category()}</dt>
				<dd class:muted={!t.category}>{t.category ?? m.detail_not_set()}</dd>
				<dt>{m.queue_col_assignee()}</dt>
				<dd class:muted={!t.assignee}>{t.assignee?.name ?? m.queue_assignee_none()}</dd>
				<dt>{m.queue_col_location()}</dt>
				<dd>{t.location.building} · {t.location.floor} · {t.location.line}</dd>
				<dt>{m.detail_f_guest()}</dt>
				<dd>{t.guest_name}</dd>
				<dt>{m.detail_f_employee()}</dt>
				<dd>{t.employee_id}</dd>
				<dt>{m.detail_f_language()}</dt>
				<dd>{languageNames[t.language as Locale] ?? t.language}</dd>
				<dt>{m.queue_col_created()}</dt>
				<dd>{fmt(t.created_at)}</dd>
				<dt>{m.detail_f_first_response()}</dt>
				<dd class:muted={!t.first_response_at}>{fmt(t.first_response_at)}</dd>
				<dt>{m.detail_f_updated()}</dt>
				<dd>{fmt(t.updated_at)}</dd>
			</dl>

			{#if can('ticket.update') || can('ticket.assign')}
				<div class="actions">
					{#if stale}
						<Alert variant="warning">
							<p class="stale">{m.detail_live_changed()}</p>
							<Button variant="secondary" onclick={() => (base = t)}>{m.detail_live_reload()}</Button>
						</Alert>
					{/if}
					{#if can('ticket.update') || (can('ticket.assign') && !selfAssignOnly)}
						<form onsubmit={save}>
							{#if can('ticket.update')}
								{#if nextStatuses.length}
									<Select label={m.queue_col_status()} options={[base?.status ?? t.status, ...nextStatuses].map(option)} bind:value={status} />
								{/if}
								<Select label={m.queue_col_priority()} placeholder={m.priority_none()} options={priorities.map(option)} bind:value={priority} />
								<Select
									label={m.detail_f_category()}
									placeholder={m.detail_not_set()}
									options={data.categories.map((c) => ({ value: String(c.id), label: c.name }))}
									bind:value={categoryId}
								/>
							{/if}
							{#if can('ticket.assign') && !selfAssignOnly}
								<Select
									label={m.queue_col_assignee()}
									options={[{ value: '', label: m.queue_assignee_none() }, ...data.assignees.map((s) => ({ value: String(s.id), label: s.name }))]}
									bind:value={assigneeId}
								/>
							{/if}
							<Button type="submit" disabled={busy}>{m.detail_save()}</Button>
						</form>
					{/if}
					{#if can('ticket.assign') && selfAssignOnly && t.assignee?.id !== data.me.id}
						<Button variant="secondary" disabled={busy} onclick={() => run(() => assignTicket(t.id, data.me.id), m.detail_saved())}>
							{m.detail_assign_me()}
						</Button>
					{/if}
					{#if actionError}<Alert variant="error">{actionError}</Alert>{/if}
				</div>
			{/if}
		</section>

		<div class="main">
			<section class="card" aria-labelledby="problem-title">
				<h2 id="problem-title">{m.detail_problem()}</h2>
				<p class="text">{t.case_details}</p>
			</section>

			{#if t.attachments.length}
				<section class="card" aria-labelledby="evidence-title">
					<h2 id="evidence-title">{m.detail_evidence()}</h2>
					<ul class="files">
						{#each t.attachments as a, i (a.id)}
							{@const src = staffFileURL(t.id, a.id)}
							<li class={a.media_type}>
								{#if a.media_type === 'image'}
									<button type="button" onclick={() => open(src, fileLabel(a, i))} aria-label={m.detail_open_file({ name: fileLabel(a, i) })}>
										<img {src} alt="" />
									</button>
								{:else}
									<!-- svelte-ignore a11y_media_has_caption: guest evidence has no captions -->
									<video {src} controls preload="metadata" aria-label={fileLabel(a, i)}></video>
								{/if}
								<span class="muted">{fileLabel(a, i)} · {size(a.size_bytes)}</span>
							</li>
						{/each}
					</ul>
				</section>
			{/if}

			<section class="card" aria-labelledby="thread-title">
				<h2 id="thread-title">{m.detail_conversation()}</h2>
				{#if t.comments.length}
					<ol class="thread">
						{#each t.comments as c (c.id)}
							<li class:guest={!c.author} class:internal={c.internal}>
								<div class="meta">
									<span class="who">{c.author?.name ?? m.detail_guest()}</span>
									{#if c.internal}<span class="tag">{m.detail_internal()}</span>{/if}
									<span class="muted">{fmt(c.created_at)}</span>
								</div>
								<p class="text">{c.body}</p>
							</li>
						{/each}
					</ol>
				{:else}
					<p class="muted">{m.detail_no_comments()}</p>
				{/if}

				{#if can('ticket.comment') && t.status !== 'closed'}
					<form class={['reply', replyMode === 'internal' && 'internal']} onsubmit={send} novalidate>
						<fieldset>
							<legend>{m.detail_reply_as()}</legend>
							<label><input type="radio" name="reply-mode" value="public" bind:group={replyMode} /> {m.detail_reply_public()}</label>
							<label><input type="radio" name="reply-mode" value="internal" bind:group={replyMode} /> {m.detail_internal()}</label>
						</fieldset>
						<TextArea
							label={m.detail_reply_label()}
							helper={replyMode === 'internal' ? m.detail_reply_internal_hint() : m.detail_reply_public_hint()}
							error={replyError}
							bind:value={reply}
						/>
						<Button type="submit" disabled={busy}>
							{replyMode === 'internal' ? m.detail_send_internal() : m.detail_send_public()}
						</Button>
					</form>
				{/if}
			</section>

			<section class="card" aria-labelledby="timeline-title">
				<h2 id="timeline-title">{m.detail_timeline()}</h2>
				<ol class="timeline">
					{#each t.timeline as ev (ev.id)}
						<li>{describe(ev)} · <time datetime={ev.created_at} class="muted">{fmt(ev.created_at)}</time></li>
					{/each}
				</ol>
			</section>
		</div>
	</div>
{/if}

<dialog bind:this={dialog} onclick={(e) => e.target === dialog && dialog.close()} aria-label={shown.alt}>
	<img src={shown.src || undefined} alt={shown.alt} />
	<form method="dialog"><Button type="submit" variant="secondary">{m.detail_close()}</Button></form>
</dialog>

<style>
	.back {
		display: inline-block;
		margin-bottom: var(--space-md);
		font: var(--font-label);
		color: var(--color-ink);
	}
	header {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-sm);
		margin-bottom: var(--space-lg);
	}
	.layout {
		display: grid;
		gap: var(--space-lg);
	}
	.main {
		display: flex;
		flex-direction: column;
		gap: var(--space-lg);
		min-width: 0;
	}
	/* Desktop: conversation on the left, details on the right and in view while scrolling. */
	@media (min-width: 1024px) {
		.layout {
			grid-template-columns: minmax(0, 2fr) minmax(280px, 1fr);
			align-items: start;
		}
		.details {
			grid-column: 2;
			grid-row: 1;
			position: sticky;
			top: var(--space-lg);
			/* With the action controls it can be taller than the window; scroll inside so Save stays reachable. */
			max-height: calc(100dvh - 2 * var(--space-lg));
			overflow-y: auto;
		}
		.main {
			grid-column: 1;
			grid-row: 1;
		}
	}
	.card {
		padding: var(--space-lg);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
		background: var(--color-canvas);
		box-shadow: var(--shadow-sm);
	}
	h2 {
		margin: 0 0 var(--space-md);
		font: var(--font-title-md);
		color: var(--color-ink);
	}
	dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: var(--space-sm) var(--space-md);
		align-items: center;
		margin: 0;
		font: var(--font-body-sm);
	}
	dt {
		font: var(--font-label);
		color: var(--color-ink);
	}
	dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.muted {
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	.text {
		margin: 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.files {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-md);
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.files li {
		display: flex;
		flex-direction: column;
		gap: var(--space-xxs);
	}
	.files li.video {
		flex-basis: 100%;
	}
	.files button {
		padding: 0;
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-md);
		background: var(--color-surface-card);
		cursor: zoom-in;
		overflow: hidden;
	}
	.files img {
		display: block;
		width: calc(var(--space-section) + var(--space-lg));
		height: calc(var(--space-section) + var(--space-lg));
		object-fit: cover;
	}
	video {
		width: 100%;
		max-height: 60vh;
		border-radius: var(--radius-md);
		background: var(--color-surface-dark);
	}
	.thread {
		display: flex;
		flex-direction: column;
		gap: var(--space-sm);
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.thread li {
		padding: var(--space-sm) var(--space-md);
		border: 1px solid transparent;
		border-radius: var(--radius-lg);
		background: var(--color-surface-card);
	}
	.thread li.guest {
		background: var(--color-accent-tint);
	}
	/* Internal notes: tint plus a dashed border and a text tag, so color is not the only signal. */
	.thread li.internal {
		border: 1px dashed var(--color-warning);
		background: var(--color-warning-tint);
	}
	.meta {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-xs);
	}
	.who {
		font: var(--font-label);
		color: var(--color-ink);
	}
	/* Muted is under 4.5:1 on surface-card and tints (DESIGN.md "Surfaces"). */
	.thread .muted {
		color: var(--color-body);
	}
	.tag {
		padding: 0 var(--space-xs);
		border-radius: var(--radius-pill);
		background: var(--color-canvas);
		font: var(--font-caption);
		color: var(--color-ink);
	}
	.thread .text {
		margin-top: var(--space-xxs);
	}
	/* Actions and the reply box sit under a hairline, below what they change. */
	.actions,
	.actions form,
	.reply {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
	}
	.actions,
	.reply {
		margin-top: var(--space-lg);
		padding-top: var(--space-lg);
		border-top: 1px solid var(--color-hairline);
	}
	fieldset {
		display: flex;
		flex-wrap: wrap;
		gap: 0 var(--space-lg);
		margin: 0;
		padding: 0;
		border: none;
	}
	legend {
		padding: 0;
		font: var(--font-label);
		color: var(--color-ink);
	}
	fieldset label {
		display: inline-flex;
		align-items: center;
		gap: var(--space-xs);
		min-height: var(--size-control);
		cursor: pointer;
	}
	fieldset input {
		width: var(--space-md);
		height: var(--space-md);
		margin: 0;
		accent-color: var(--color-primary);
	}
	/* Internal mode: the text box takes the internal-note look (FR-T6); labels and errors stay on canvas for contrast. */
	.reply.internal :global(textarea) {
		border-style: dashed;
		background: var(--color-warning-tint);
	}
	.reply.internal :global(textarea:not(:focus)) {
		border-color: var(--color-warning);
	}
	.stale {
		margin: 0 0 var(--space-xs);
	}
	.timeline {
		display: flex;
		flex-direction: column;
		gap: var(--space-sm);
		margin: 0;
		padding: 0;
		list-style: none;
		font: var(--font-body-sm);
	}
	.timeline li {
		padding-left: var(--space-md);
		border-left: 2px solid var(--color-hairline);
		overflow-wrap: anywhere;
	}
	dialog {
		max-width: min(92vw, 1200px);
		padding: var(--space-md);
		border: none;
		border-radius: var(--radius-xl);
		box-shadow: var(--shadow-md);
	}
	dialog::backdrop {
		background: color-mix(in srgb, var(--color-surface-dark) 70%, transparent);
	}
	dialog img {
		display: block;
		max-width: 100%;
		max-height: 75vh;
		margin: 0 auto var(--space-md);
		border-radius: var(--radius-md);
	}
	dialog form {
		display: flex;
		justify-content: flex-end;
	}
</style>
