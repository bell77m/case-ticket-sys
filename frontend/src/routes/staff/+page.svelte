<!-- Staff ticket queue (T1.18). Filters live in the URL, so a link can open a filtered queue (T3.03). -->
<script lang="ts">
	import { onMount } from 'svelte';
	import { goto, invalidate } from '$app/navigation';
	import { page } from '$app/state';
	import type { QueueItem } from '$lib/api';
	import Badge, { labels, priorities, statuses } from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import Select from '$lib/components/Select.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import { coalesce, onTicketEvent } from '$lib/live.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';

	let { data } = $props();
	const params = $derived(page.url.searchParams);
	const queue = $derived(data.queue);
	const pages = $derived(Math.max(1, Math.ceil(queue.total / queue.page_size)));
	let search = $state(page.url.searchParams.get('q') ?? '');

	// Live (FR-P3): any ticket change reloads this page of the queue in place, same filters, no scroll jump.
	onMount(() => onTicketEvent(coalesce(() => invalidate('app:queue'))));

	const statusOptions = [
		{ value: 'open', label: m.queue_status_open() },
		{ value: 'all', label: m.queue_all() },
		...statuses.map((s) => ({ value: s, label: labels[s]() }))
	];
	const priorityOptions = [
		{ value: '', label: m.queue_all() },
		...priorities.map((p) => ({ value: p, label: labels[p]() })),
		{ value: 'none', label: m.priority_none() }
	];
	const assigneeOptions = [
		{ value: '', label: m.queue_all() },
		{ value: 'me', label: m.queue_assignee_me() },
		{ value: 'none', label: m.queue_assignee_none() }
	];

	// Changing a filter goes back to page 1.
	function setParam(key: string, value: string) {
		const next = new URLSearchParams(page.url.searchParams);
		if (value) next.set(key, value);
		else next.delete(key);
		if (key !== 'page') next.delete('page');
		goto(`?${next}`, { keepFocus: true, noScroll: key !== 'page', replaceState: key !== 'page' });
	}

	// Gregorian calendar in every language, including Thai (open question in docs/PLAN.md).
	const dateFmt = new Intl.DateTimeFormat(getLocale(), { dateStyle: 'medium', timeStyle: 'short', calendar: 'gregory' });
	const fmt = (iso: string) => dateFmt.format(new Date(iso));
	const place = (t: QueueItem) => `${t.location.building} · ${t.location.floor} · ${t.location.line}`;
</script>

<header>
	<h1>{m.queue_heading()}</h1>
	<span class="muted">{queue.total === 1 ? m.queue_total_one() : m.queue_total({ count: String(queue.total) })}</span>
</header>

<div class="filters">
	<Select
		label={m.queue_filter_status()}
		options={statusOptions}
		value={params.get('status') ?? 'open'}
		onchange={(e) => setParam('status', e.currentTarget.value === 'open' ? '' : e.currentTarget.value)}
	/>
	<Select
		label={m.queue_filter_priority()}
		options={priorityOptions}
		value={params.get('priority') ?? ''}
		onchange={(e) => setParam('priority', e.currentTarget.value)}
	/>
	<Select
		label={m.queue_filter_assignee()}
		options={assigneeOptions}
		value={params.get('assignee') ?? ''}
		onchange={(e) => setParam('assignee', e.currentTarget.value)}
	/>
	<form
		class="search"
		role="search"
		onsubmit={(e) => {
			e.preventDefault();
			setParam('q', search.trim());
		}}
	>
		<div class="row">
			<TextInput label={m.queue_search()} type="search" bind:value={search} maxlength={200} aria-describedby="search-help" />
			<Button type="submit" variant="secondary">{m.queue_search()}</Button>
		</div>
		<p id="search-help" class="muted">{m.queue_search_helper()}</p>
	</form>
</div>

{#if queue.items.length}
	<!-- Tablet and desktop: a table. Phones: the same tickets as stacked cards. -->
	<table>
		<thead>
			<tr>
				<th scope="col">{m.queue_col_ticket()}</th>
				<th scope="col">{m.queue_col_summary()}</th>
				<th scope="col">{m.queue_col_status()}</th>
				<th scope="col">{m.queue_col_priority()}</th>
				<th scope="col">{m.queue_col_location()}</th>
				<th scope="col">{m.queue_col_assignee()}</th>
				<th scope="col">{m.queue_col_created()}</th>
			</tr>
		</thead>
		<tbody>
			{#each queue.items as t (t.id)}
				<tr>
					<td class="num">{m.queue_ticket_no({ id: String(t.id) })}</td>
					<td>
						<a href="/staff/tickets/{t.id}">{t.summary}</a>
						<div class="muted">{t.employee_id}</div>
					</td>
					<td><Badge kind="status" value={t.status} /></td>
					<td>
						{#if t.priority}<Badge kind="priority" value={t.priority} />{:else}<span class="muted">{m.priority_none()}</span>{/if}
					</td>
					<td>{place(t)}</td>
					<td>{t.assignee?.name ?? m.queue_assignee_none()}</td>
					<td class="nowrap">{fmt(t.created_at)}</td>
				</tr>
			{/each}
		</tbody>
	</table>

	<ul class="cards">
		{#each queue.items as t (t.id)}
			<li>
				<div class="badges">
					<span class="num">{m.queue_ticket_no({ id: String(t.id) })}</span>
					<Badge kind="status" value={t.status} />
					{#if t.priority}<Badge kind="priority" value={t.priority} />{/if}
				</div>
				<a href="/staff/tickets/{t.id}">{t.summary}</a>
				<div class="muted">{place(t)} · {fmt(t.created_at)}</div>
				<div class="muted">{t.employee_id} · {t.assignee?.name ?? m.queue_assignee_none()}</div>
			</li>
		{/each}
	</ul>

	{#if pages > 1}
		<nav aria-label={m.queue_pages()}>
			<Button variant="secondary" disabled={queue.page <= 1} onclick={() => setParam('page', String(queue.page - 1))}>
				{m.queue_prev()}
			</Button>
			<span>{m.queue_page({ page: String(queue.page), pages: String(pages) })}</span>
			<Button variant="secondary" disabled={queue.page >= pages} onclick={() => setParam('page', String(queue.page + 1))}>
				{m.queue_next()}
			</Button>
		</nav>
	{/if}
{:else}
	<p class="empty">{m.queue_empty()}</p>
{/if}

<style>
	header {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--space-sm);
		margin-bottom: var(--space-lg);
	}
	.muted {
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	.filters {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 180px), 1fr));
		align-items: start;
		gap: var(--space-md);
		margin-bottom: var(--space-lg);
	}
	.search {
		grid-column: 1 / -1;
	}
	.search .row {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: var(--space-xs);
	}
	.search .row > :global(.field) {
		flex: 1 1 260px;
	}
	.search p {
		margin: var(--space-xxs) 0 0;
	}
	table {
		width: 100%;
		border-collapse: separate;
		border-spacing: 0;
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
		box-shadow: var(--shadow-sm);
		font: var(--font-body-sm);
		overflow: hidden;
	}
	th {
		padding: var(--space-sm) var(--space-md);
		background: var(--color-surface-soft);
		font: var(--font-label);
		color: var(--color-ink);
		text-align: start;
	}
	td {
		padding: var(--space-sm) var(--space-md);
		border-top: 1px solid var(--color-hairline);
		vertical-align: top;
	}
	td a,
	.cards a {
		font: var(--font-title-sm);
		color: var(--color-ink);
		overflow-wrap: anywhere;
	}
	.num {
		font: var(--font-label);
		color: var(--color-ink);
		white-space: nowrap;
	}
	.nowrap {
		white-space: nowrap;
	}
	.cards {
		display: none;
		margin: 0;
		padding: 0;
		list-style: none;
		flex-direction: column;
		gap: var(--space-sm);
	}
	.cards li {
		display: flex;
		flex-direction: column;
		gap: var(--space-xxs);
		padding: var(--space-md);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
		box-shadow: var(--shadow-sm);
	}
	.badges {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-xs);
		margin-bottom: var(--space-xxs);
	}
	nav {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-md);
		margin-top: var(--space-lg);
		font: var(--font-body-sm);
	}
	.empty {
		padding: var(--space-xl);
		border-radius: var(--radius-lg);
		background: var(--color-surface-soft);
		text-align: center;
		color: var(--color-muted);
	}
	/* Seven columns only fit from 1024px; narrower, the summary column shrinks to one word per line. */
	@media (max-width: 1023px) {
		table {
			display: none;
		}
		.cards {
			display: flex;
		}
	}
</style>
