<!-- Activity log rows (T3.06, FR-L1), shared by /staff/admin/activity and its print page. Layout: DESIGN.md "Activity log". -->
<script lang="ts">
	import { actionLabel, actorName, change } from '$lib/activity';
	import type { ActivityItem } from '$lib/api';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';

	type Props = {
		items: ActivityItem[];
		/** The print page: the table at any width, ticket numbers as text (a PDF link would lead nowhere). */
		print?: boolean;
		/** IANA zone for the times; the print page passes the export's, since Gotenberg runs in UTC. */
		timeZone?: string;
	};
	let { items, print = false, timeZone }: Props = $props();

	// Gregorian calendar in every language, including Thai (frontend.md). Seconds: several rows can share a minute.
	const dateFmt = $derived(
		new Intl.DateTimeFormat(getLocale(), { dateStyle: 'medium', timeStyle: 'medium', calendar: 'gregory', timeZone })
	);
	const fmt = (iso: string) => dateFmt.format(new Date(iso));
</script>

{#snippet ticket(id: number)}
	{#if print}{m.queue_ticket_no({ id: String(id) })}{:else}<a href="/staff/tickets/{id}">{m.queue_ticket_no({ id: String(id) })}</a>{/if}
{/snippet}

<!-- From 1024px (and in print): a table. Narrower: the same rows as stacked cards. -->
<table class={{ print }}>
	<thead>
		<tr>
			<th scope="col">{m.activity_col_time()}</th>
			<th scope="col">{m.activity_col_actor()}</th>
			<th scope="col">{m.activity_col_action()}</th>
			<th scope="col">{m.activity_col_ticket()}</th>
			<th scope="col">{m.activity_col_target()}</th>
			<th scope="col">{m.activity_col_ip()}</th>
		</tr>
	</thead>
	<tbody>
		{#each items as it (it.id)}
			{@const c = change(it)}
			<tr>
				<td class="nowrap">{fmt(it.created_at)}</td>
				<td>{actorName(it.actor)}</td>
				<td>
					<span class="action">{actionLabel(it.action)}</span>
					{#if c}<div class="muted">{c}</div>{/if}
				</td>
				<td class="nowrap">{#if it.ticket_id}{@render ticket(it.ticket_id)}{/if}</td>
				<td class="wrap">{it.target ?? ''}</td>
				<td class="wrap">{it.ip ?? ''}</td>
			</tr>
		{/each}
	</tbody>
</table>

{#if !print}
	<ul class="cards">
		{#each items as it (it.id)}
			{@const c = change(it)}
			<li>
				<div class="top">
					<span class="action">{actionLabel(it.action)}</span>
					{#if it.ticket_id}{@render ticket(it.ticket_id)}{/if}
				</div>
				{#if c}<div class="wrap">{c}</div>{/if}
				<div class="muted">{actorName(it.actor)} · {fmt(it.created_at)}</div>
				{#if it.target || it.ip}<div class="muted wrap">{[it.target, it.ip].filter(Boolean).join(' · ')}</div>{/if}
			</li>
		{/each}
	</ul>
{/if}

<style>
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
	.action {
		font: var(--font-label);
		color: var(--color-ink);
	}
	a {
		font: var(--font-label);
		color: var(--color-ink);
	}
	.muted {
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	.nowrap {
		white-space: nowrap;
	}
	/* Targets hold usernames, names and export filters (JSON); IPv6 addresses are long too. */
	.wrap {
		overflow-wrap: anywhere;
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
		font: var(--font-body-sm);
	}
	.top {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		gap: var(--space-xs);
	}
	/* Six columns fit from 1024px. */
	@media (max-width: 1023px) {
		table:not(.print) {
			display: none;
		}
		.cards {
			display: flex;
		}
	}
	/* Print (180mm): no shadow, tighter cells, times may wrap, and Target keeps room for usernames and filters.
	   A row never splits across pages; the header row repeats on each. */
	table.print {
		box-shadow: none;
	}
	.print th,
	.print td {
		padding: var(--space-xs) var(--space-sm);
		white-space: normal;
	}
	.print th:first-child {
		width: 16%;
	}
	.print th:nth-child(5) {
		width: 26%;
	}
	.print tr {
		break-inside: avoid;
	}
</style>
