<!-- Guest tracking page (T1.15, FR-G3–G5). The token is read from the URL after "#". -->
<script lang="ts">
	import { onMount } from 'svelte';
	import { ApiError, confirmTrack, getTrack, replyTrack, trackFileURL, type TrackView } from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import Badge from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import TextArea from '$lib/components/TextArea.svelte';
	import { showToast } from '$lib/components/toast-state.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';

	let token = $state('');
	let view = $state<TrackView | null>(null);
	let notFound = $state(false);
	let loadError = $state('');
	let reply = $state('');
	let replyError = $state<string | undefined>();
	let busy = $state(false);
	let fileURLs = $state<Record<number, string>>({});

	// Gregorian calendar in every language, including Thai (open question in docs/PLAN.md).
	const dateFmt = new Intl.DateTimeFormat(getLocale(), { dateStyle: 'medium', timeStyle: 'short', calendar: 'gregory' });
	const fmt = (iso: string) => dateFmt.format(new Date(iso));

	// A truncated or edited link can hold a broken % escape; treat it as no token.
	function readToken() {
		try {
			return decodeURIComponent(location.hash.slice(1));
		} catch {
			return '';
		}
	}

	async function load() {
		token = readToken();
		notFound = false;
		loadError = '';
		if (!token) {
			notFound = true;
			return;
		}
		try {
			view = await getTrack(token, getLocale());
		} catch (err) {
			view = null;
			if (err instanceof ApiError && err.status === 404) notFound = true;
			else loadError = m.err_network();
		}
	}

	onMount(() => {
		load();
		addEventListener('hashchange', load);
		return () => {
			removeEventListener('hashchange', load);
			Object.values(fileURLs).forEach((u) => URL.revokeObjectURL(u));
		};
	});

	async function send(event: SubmitEvent) {
		event.preventDefault();
		if (!view) return;
		replyError = reply.trim() ? undefined : m.err_required();
		if (replyError) return;
		busy = true;
		try {
			await replyTrack(token, reply);
			reply = '';
			await load(); // status may have changed (waiting or resolved goes back to in progress)
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') replyError = m.err_invalid_characters();
			else if (err instanceof ApiError && err.code === 'ticket.closed') await load();
			else replyError = m.err_network();
		} finally {
			busy = false;
		}
	}

	async function confirmFix() {
		busy = true;
		try {
			await confirmTrack(token);
			showToast(m.track_confirmed());
			await load();
		} catch {
			await load();
		} finally {
			busy = false;
		}
	}

	async function showFile(id: number) {
		try {
			fileURLs[id] = await trackFileURL(token, id);
		} catch {
			showToast(m.err_network());
		}
	}

	const label = (a: TrackView['attachments'][number], i: number) =>
		a.media_type === 'image' ? m.track_photo({ n: String(i + 1) }) : m.track_video({ n: String(i + 1) });
	const size = (b: number) => `${(b / 1024 / 1024).toFixed(1)} MB`;
</script>

<div class="page">
	{#if notFound}
		<Alert variant="error">{m.track_not_found()}</Alert>
		<a href="/report">{m.track_new_ticket()}</a>
	{:else if loadError}
		<Alert variant="error">{loadError}</Alert>
	{:else if view}
		<header>
			<h1>{m.track_heading({ id: String(view.ticket_id) })}</h1>
			<Badge kind="status" value={view.status} />
		</header>

		{#if view.status === 'resolved'}
			<Card>
				<div class="stack">
					<p class="question">{m.track_resolved_question()}</p>
					<Button onclick={confirmFix} disabled={busy}>{m.track_confirm()}</Button>
					<p class="hint">{m.track_resolved_hint()}</p>
				</div>
			</Card>
		{:else if view.status === 'closed'}
			<Alert variant="success">{m.track_closed()}</Alert>
		{/if}

		<Card title={m.track_details()}>
			<dl>
				<dt>{m.track_location()}</dt>
				<dd>{view.location.building} · {view.location.floor} · {view.location.line}</dd>
				<dt>{m.track_submitted()}</dt>
				<dd>{fmt(view.created_at)}</dd>
			</dl>
			<p class="details">{view.case_details}</p>
		</Card>

		{#if view.attachments.length}
			<Card title={m.track_evidence()}>
				<ul class="files">
					{#each view.attachments as a, i (a.id)}
						<li>
							<span>{label(a, i)} <span class="muted">{size(a.size_bytes)}</span></span>
							{#if fileURLs[a.id]}
								{#if a.media_type === 'image'}
									<img src={fileURLs[a.id]} alt={label(a, i)} />
								{:else}
									<!-- svelte-ignore a11y_media_has_caption: guest evidence has no captions -->
									<video src={fileURLs[a.id]} controls aria-label={label(a, i)}></video>
								{/if}
							{:else}
								<Button variant="ghost" onclick={() => showFile(a.id)} aria-label={m.track_view_file({ name: label(a, i) })}>
									{m.track_view()}
								</Button>
							{/if}
						</li>
					{/each}
				</ul>
			</Card>
		{/if}

		<Card title={m.track_replies()}>
			{#if view.comments.length}
				<ul class="thread">
					{#each view.comments as c (c.id)}
						<li class={c.from}>
							<span class="who">{c.from === 'guest' ? m.track_from_you() : m.track_from_it()}</span>
							<span class="muted">{fmt(c.created_at)}</span>
							<p>{c.body}</p>
						</li>
					{/each}
				</ul>
			{:else}
				<p class="muted">{m.track_no_replies()}</p>
			{/if}

			{#if view.status !== 'closed'}
				<form class="stack" onsubmit={send} novalidate>
					<TextArea label={m.track_reply_label()} bind:value={reply} maxlength={5000} error={replyError} />
					<div class="actions"><Button type="submit" disabled={busy}>{m.track_send()}</Button></div>
				</form>
			{/if}
		</Card>
	{/if}
</div>

<style>
	.page {
		display: flex;
		flex-direction: column;
		gap: var(--space-lg);
		max-width: 640px;
		margin: 0 auto;
	}
	header {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-sm);
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
	}
	.question {
		margin: 0;
		font: var(--font-title-sm);
		color: var(--color-ink);
	}
	.hint,
	.muted {
		margin: 0;
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: var(--space-xs) var(--space-md);
		margin: 0 0 var(--space-md);
		font: var(--font-body-sm);
	}
	dt {
		font: var(--font-label);
		color: var(--color-ink);
	}
	dd {
		margin: 0;
	}
	.details {
		margin: 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.files,
	.thread {
		margin: 0 0 var(--space-md);
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: var(--space-sm);
	}
	.files li {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-xs);
	}
	img,
	video {
		max-width: 100%;
		border-radius: var(--radius-md);
	}
	.thread li {
		padding: var(--space-sm) var(--space-md);
		border-radius: var(--radius-lg);
		background: var(--color-surface-card);
	}
	.thread li.guest {
		background: var(--color-accent-tint);
	}
	/* Muted is under 4.5:1 on surface-card and tints (DESIGN.md "Surfaces"). */
	.thread .muted {
		color: var(--color-body);
	}
	.who {
		font: var(--font-label);
		color: var(--color-ink);
		margin-right: var(--space-xs);
	}
	.thread p {
		margin: var(--space-xxs) 0 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.actions {
		display: flex;
		justify-content: flex-end;
	}
</style>
