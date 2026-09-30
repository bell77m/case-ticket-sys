<!-- The 7 summary cards of the reports dashboard and its print page (FR-P1, FR-P4). Layout: DESIGN.md "Reports dashboard". -->
<script lang="ts">
	import type { Report, ReportCard } from '$lib/api';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';
	import { formatDuration } from '$lib/report-summary';

	/** links: each card opens the queue with exactly its tickets (the dashboard; the PDF has none).
	 *  scope: the report's building, category_id and tz, as URL parameters, added to every link. */
	let { report, links = true, scope = '' }: { report: Report; links?: boolean; scope?: string } = $props();

	// Intl in the viewer's language (FR-I5).
	const locale = getLocale();
	const num = new Intl.NumberFormat(locale).format;
	const signed = new Intl.NumberFormat(locale, { signDisplay: 'exceptZero' }).format;

	// Summary cards in REQUIREMENTS §5 order. Each link opens the queue with the card's own tickets, so the queue
	// total equals the card (FR-P1; TestQueueMatchesReportCards_FRP1): open counts use the state at the period end
	// (as_of), New and Resolved the period on created or resolved time, and a median links to the tickets it is
	// measured over (DESIGN.md "Reports dashboard").
	const link = (extra: Record<string, string>) => {
		const p = new URLSearchParams(scope);
		for (const [k, v] of Object.entries(extra)) p.set(k, v);
		return `/staff?${p}`;
	};
	const cards = $derived.by(() => {
		const c = report.cards;
		const { from, to } = report.period;
		const open = { status: 'open', as_of: to };
		const created = link({ status: 'all', by: 'created', from, to });
		const resolved = link({ status: 'all', by: 'resolved', from, to });
		const tile = (key: string, label: string, card: ReportCard<number | null>, isDuration: boolean, href?: string) => {
			const diff = card.value === null || card.previous === null ? null : card.value - card.previous;
			const change =
				diff === null ? null : diff === 0 ? num(0) : isDuration ? formatDuration(diff, locale, 'exceptZero') : signed(diff);
			const value = card.value === null ? m.reports_no_data() : isDuration ? formatDuration(card.value, locale) : num(card.value);
			return { key, label, value, href: links ? href : undefined, diff, change, hasValue: card.value !== null };
		};
		return [
			tile('open', m.reports_card_open(), c.open, false, link(open)),
			tile('unassigned', m.reports_card_unassigned(), c.unassigned, false, link({ ...open, assignee: 'none' })),
			tile('urgent', m.reports_card_urgent(), c.urgent_open, false, link({ ...open, priority: 'urgent' })),
			tile('new', m.reports_card_new(), c.new, false, created),
			tile('resolved', m.reports_card_resolved(), c.resolved, false, resolved),
			tile('first', m.reports_card_first_response(), c.first_response_median_seconds, true, created),
			tile('resolution', m.reports_card_resolution(), c.resolution_median_seconds, true, resolved)
		];
	});
</script>

<ul class="cards">
	{#each cards as c (c.key)}
		<li>
			<svelte:element this={c.href ? 'a' : 'div'} class="tile" href={c.href}>
				<span class="tile-label">{c.label}</span>
				<span class="tile-value">{c.value}</span>
				{#if c.hasValue}
					<span class="tile-change">
						{#if c.change === null}
							{m.reports_previous_no_data()}
						{:else}
							{#if c.diff}
								<svg class={['glyph', c.diff < 0 && 'down']} viewBox="0 0 12 12" aria-hidden="true"><path d="M6 2 11 10H1z" /></svg>
							{/if}
							<span class="delta">{c.change}</span>
							{m.reports_vs_previous()}
						{/if}
					</span>
				{/if}
			</svelte:element>
		</li>
	{/each}
</ul>

<style>
	.cards {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: var(--space-sm);
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.tile {
		display: flex;
		flex-direction: column;
		gap: var(--space-xxs);
		height: 100%;
		padding: var(--space-md);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
		background: var(--color-canvas);
		box-shadow: var(--shadow-sm);
		color: var(--color-body);
		text-decoration: none;
		overflow-wrap: anywhere;
		break-inside: avoid;
	}
	a.tile:active {
		background: var(--color-surface-soft);
	}
	.tile-label {
		font: var(--font-label);
	}
	/* Proportional figures (the font default), same sans as everything else. */
	.tile-value {
		font: var(--font-title-lg);
		color: var(--color-ink);
	}
	.tile-change {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-xxs);
		margin-top: auto;
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	.delta {
		font-weight: 600;
		color: var(--color-body);
	}
	.glyph {
		width: var(--space-sm);
		height: var(--space-sm);
		fill: currentColor;
	}
	.glyph.down {
		transform: rotate(180deg);
	}
	@media (min-width: 768px) {
		.cards {
			grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
			gap: var(--space-md);
		}
		.tile-value {
			font: var(--font-display-sm);
		}
	}
</style>
