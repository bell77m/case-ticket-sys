<!-- Reports dashboard (T3.03, FR-P1, FR-P2, FR-I5). Layout: DESIGN.md "Reports dashboard". -->
<script lang="ts">
	import { onMount, tick } from 'svelte';
	import type { ChartConfiguration, TooltipItem } from 'chart.js';
	import { goto, invalidate } from '$app/navigation';
	import { navigating, page } from '$app/state';
	import type { ReportCard } from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import { labels } from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import Chart, { cssVar } from '$lib/components/Chart.svelte';
	import Select from '$lib/components/Select.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import WrittenSummary from '$lib/components/WrittenSummary.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';
	import { coalesce, onTicketEvent } from '$lib/live.svelte';
	import { formatDuration } from '$lib/report-summary';

	let { data } = $props();
	const report = $derived(data.allowed ? data.report : null);
	const busy = $derived(navigating.to?.route.id === '/staff/reports');

	// Live (FR-P3): any ticket change refetches with the same filters. The previous render stays until the new data
	// lands; only filter changes dim it (busy), so a busy floor does not make the dashboard blink.
	onMount(() => onTicketEvent(coalesce(() => invalidate('app:reports'))));

	// Intl in the viewer's language (FR-I5). Gregorian calendar in every language, as in the queue.
	const locale = getLocale();
	const num = new Intl.NumberFormat(locale).format;
	const signed = new Intl.NumberFormat(locale, { signDisplay: 'exceptZero' }).format;
	const hours = new Intl.NumberFormat(locale, { style: 'unit', unit: 'hour', unitDisplay: 'short', maximumSignificantDigits: 3 }).format;
	// API dates are plain YYYY-MM-DD, read as UTC midnight, so they are formatted in UTC.
	const dayShort = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', timeZone: 'UTC', calendar: 'gregory' });
	const dayLong = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC', calendar: 'gregory' });
	const short = (d: string) => dayShort.format(new Date(d));
	const long = (d: string) => dayLong.format(new Date(d));
	const duration = (s: number | null) => (s === null ? m.reports_no_data() : formatDuration(s, locale));

	// Filters (FR-P2) go to the URL. A changed date applies at once; selects only through Apply (frontend.md).
	function apply(form: HTMLFormElement) {
		const next = new URLSearchParams();
		for (const [key, value] of new FormData(form)) if (value) next.set(key, String(value));
		goto(`?${next}`, { keepFocus: true, noScroll: true, replaceState: true });
	}
	// Typing a year fires change at 0002, 0020, 0202: wait for a full date the API accepts.
	function applyDates(form: HTMLFormElement | null) {
		const from = String(new FormData(form!).get('from'));
		const to = String(new FormData(form!).get('to'));
		if (form && from >= '2000' && to >= '2000' && from <= to) apply(form);
	}

	// Summary cards in REQUIREMENTS §5 order. Links open the queue's closest filter; it has no date, building or
	// category filter and shows tickets as they are now (DESIGN.md "Reports dashboard").
	const open = '/staff?status=open';
	const cards = $derived.by(() => {
		if (!report) return [];
		const c = report.cards;
		const tile = (key: string, label: string, card: ReportCard<number | null>, isDuration: boolean, href?: string) => {
			const diff = card.value === null || card.previous === null ? null : card.value - card.previous;
			const change =
				diff === null ? null : diff === 0 ? num(0) : isDuration ? formatDuration(diff, locale, 'exceptZero') : signed(diff);
			const value = card.value === null ? m.reports_no_data() : isDuration ? formatDuration(card.value, locale) : num(card.value);
			return { key, label, value, href, diff, change, hasValue: card.value !== null };
		};
		return [
			tile('open', m.reports_card_open(), c.open, false, open),
			tile('unassigned', m.reports_card_unassigned(), c.unassigned, false, `${open}&assignee=none`),
			tile('urgent', m.reports_card_urgent(), c.urgent_open, false, `${open}&priority=urgent`),
			tile('new', m.reports_card_new(), c.new, false, '/staff?status=new'),
			tile('resolved', m.reports_card_resolved(), c.resolved, false, '/staff?status=resolved'),
			tile('first', m.reports_card_first_response(), c.first_response_median_seconds, true),
			tile('resolution', m.reports_card_resolution(), c.resolution_median_seconds, true)
		];
	});

	// Location drill-down: building → floor → line, by name (names are in the viewer's language).
	let drill = $state<{ building?: string; floor?: string }>({});
	const building = $derived(report?.by_location.find((b) => b.building === drill.building));
	const floor = $derived(building?.floors.find((f) => f.floor === drill.floor));
	async function drillTo(next: { building?: string; floor?: string }) {
		drill = next;
		await tick();
		(document.getElementById('drill-back') ?? document.getElementById('g-location'))?.focus();
	}

	type Row = { key: string; cells: string[]; drill?: () => void };
	type Graph = {
		id: string;
		title: string;
		empty: boolean;
		config: ChartConfiguration;
		/** Bars per row for horizontal charts: the plot grows with them. */
		rows?: number;
		head: string[];
		body: Row[];
	};

	// Line charts: one tooltip for every series at the hovered x; legend keys drawn as lines.
	const lineOptions = {
		interaction: { mode: 'index', intersect: false },
		plugins: { legend: { labels: { usePointStyle: true, pointStyle: 'line' } } }
	} as const;
	const valueAxis = { beginAtZero: true, ticks: { precision: 0 } };
	const noGrid = { grid: { display: false } };

	/** One series of bars, all `--chart-1`, no legend. Horizontal bars carry the category on y. */
	function bars(names: string[], counts: number[], horizontal: boolean, onClick?: (i: number) => void): ChartConfiguration {
		return {
			type: 'bar',
			data: { labels: names, datasets: [{ label: m.reports_col_tickets(), data: counts, backgroundColor: cssVar('--chart-1') }] },
			options: {
				indexAxis: horizontal ? 'y' : 'x',
				interaction: { mode: 'index', intersect: false },
				plugins: { legend: { display: false } },
				scales: horizontal ? { x: valueAxis, y: noGrid } : { x: noGrid, y: valueAxis },
				onClick: onClick && ((_e, els) => els[0] && onClick(els[0].index))
			}
		};
	}

	const graphs = $derived.by((): Graph[] => {
		if (!report) return [];
		const r = report;
		const [c1, c2] = [cssVar('--chart-1'), cssVar('--chart-2')];
		const series = (color: string) => ({ borderColor: color, backgroundColor: color });

		const daily = r.opened_resolved_daily;
		const weeks = r.weekly_medians;
		const toHours = (s: number | null) => (s === null ? null : s / 3600);

		const statusColor: Record<string, string> = {
			new: cssVar('--color-brand-accent'),
			in_progress: cssVar('--color-warning'),
			waiting: cssVar('--color-badge-violet'),
			resolved: cssVar('--color-success'),
			closed: cssVar('--color-muted-soft')
		};
		const priorityName = (p: string | null) => (p ? labels[p as keyof typeof labels]() : m.priority_none());
		const categoryName = (n: string | null) => n ?? m.reports_no_category();

		// Location: the level on show, and what a click on one of its bars opens.
		const level = floor
			? { head: m.form_line(), items: floor.lines.map((l) => ({ name: l.line, count: l.count })), open: undefined }
			: building
				? {
						head: m.form_floor(),
						items: building.floors.map((f) => ({ name: f.floor, count: f.count })),
						open: (i: number) => drillTo({ building: building.building, floor: building.floors[i].floor })
					}
				: {
						head: m.form_building(),
						items: r.by_location.map((b) => ({ name: b.building, count: b.count })),
						open: (i: number) => drillTo({ building: r.by_location[i].building })
					};

		const priorities = ['urgent', 'high', 'medium', 'low', 'none'] as const;

		return [
			{
				id: 'daily',
				title: m.reports_graph_daily(),
				empty: daily.every((d) => d.opened === 0 && d.resolved === 0),
				config: {
					type: 'line',
					data: {
						labels: daily.map((d) => short(d.date)),
						datasets: [
							{ label: m.reports_series_opened(), data: daily.map((d) => d.opened), ...series(c1) },
							{ label: m.reports_series_resolved(), data: daily.map((d) => d.resolved), ...series(c2) }
						]
					},
					options: { ...lineOptions, scales: { x: noGrid, y: valueAxis } }
				},
				head: [m.reports_col_date(), m.reports_series_opened(), m.reports_series_resolved()],
				body: daily.map((d) => ({ key: d.date, cells: [long(d.date), num(d.opened), num(d.resolved)] }))
			},
			{
				id: 'status',
				title: m.reports_graph_status(),
				empty: r.open_by_status.every((s) => s.count === 0),
				config: {
					type: 'doughnut',
					data: {
						labels: r.open_by_status.map((s) => labels[s.status]()),
						datasets: [
							{
								label: m.reports_card_open(),
								data: r.open_by_status.map((s) => s.count),
								backgroundColor: r.open_by_status.map((s) => statusColor[s.status])
							}
						]
					},
					options: { plugins: { legend: { position: 'bottom' } } }
				},
				head: [m.queue_col_status(), m.reports_card_open()],
				body: r.open_by_status.map((s) => ({ key: s.status, cells: [labels[s.status](), num(s.count)] }))
			},
			{
				id: 'priority',
				title: m.reports_graph_priority(),
				empty: r.open_by_priority.every((p) => p.count === 0),
				config: bars(
					r.open_by_priority.map((p) => priorityName(p.priority)),
					r.open_by_priority.map((p) => p.count),
					false
				),
				head: [m.queue_col_priority(), m.reports_card_open()],
				body: r.open_by_priority.map((p) => ({ key: p.priority ?? 'none', cells: [priorityName(p.priority), num(p.count)] }))
			},
			{
				id: 'category',
				title: m.reports_graph_category(),
				empty: r.by_category.length === 0,
				rows: r.by_category.length,
				config: bars(
					r.by_category.map((c) => categoryName(c.name)),
					r.by_category.map((c) => c.count),
					true
				),
				head: [m.detail_f_category(), m.reports_col_tickets()],
				body: r.by_category.map((c) => ({ key: String(c.id), cells: [categoryName(c.name), num(c.count)] }))
			},
			{
				id: 'location',
				title: m.reports_graph_location(),
				empty: level.items.length === 0,
				rows: level.items.length,
				config: bars(
					level.items.map((i) => i.name),
					level.items.map((i) => i.count),
					true,
					level.open
				),
				head: [level.head, m.reports_col_tickets()],
				body: level.items.map((it, i) => ({
					key: it.name,
					cells: [it.name, num(it.count)],
					drill: level.open && (() => level.open(i))
				}))
			},
			{
				id: 'workload',
				title: m.reports_graph_workload(),
				empty: r.workload.length === 0,
				rows: r.workload.length,
				config: {
					type: 'bar',
					data: {
						labels: r.workload.map((w) => w.assignee.name),
						// Stacked from the baseline, most urgent first; a 2px canvas border is the gap between segments.
						datasets: priorities.map((p) => ({
							label: priorityName(p === 'none' ? null : p),
							data: r.workload.map((w) => w[p]),
							backgroundColor: cssVar(`--chart-priority-${p}`),
							borderColor: cssVar('--color-canvas'),
							borderWidth: 2
						}))
					},
					options: {
						indexAxis: 'y',
						interaction: { mode: 'index', intersect: false },
						scales: { x: { ...valueAxis, stacked: true }, y: { ...noGrid, stacked: true } }
					}
				},
				head: [m.reports_col_staff(), ...priorities.map((p) => priorityName(p === 'none' ? null : p)), m.reports_col_total()],
				body: r.workload.map((w) => ({
					key: String(w.assignee.id),
					cells: [w.assignee.name, ...priorities.map((p) => num(w[p])), num(priorities.reduce((sum, p) => sum + w[p], 0))]
				}))
			},
			{
				id: 'medians',
				title: m.reports_graph_medians(),
				empty: weeks.every((w) => w.first_response_seconds === null && w.resolution_seconds === null),
				config: {
					type: 'line',
					data: {
						labels: weeks.map((w) => short(w.week_start)),
						// Few points with gaps: markers at rest, or a lone week between empty ones would not show.
						datasets: [
							{
								label: m.reports_series_first_response(),
								data: weeks.map((w) => toHours(w.first_response_seconds)),
								pointRadius: 4,
								...series(c1)
							},
							{
								label: m.reports_series_resolution(),
								data: weeks.map((w) => toHours(w.resolution_seconds)),
								pointRadius: 4,
								...series(c2)
							}
						]
					},
					// One y-axis in hours for both series; never two.
					options: {
						...lineOptions,
						plugins: {
							...lineOptions.plugins,
							tooltip: {
								callbacks: {
									label: (t: TooltipItem<'line'>) => `${t.dataset.label}: ${formatDuration(t.parsed.y! * 3600, locale)}`
								}
							}
						},
						scales: { x: noGrid, y: { beginAtZero: true, ticks: { callback: (v) => hours(Number(v)) } } }
					}
				},
				head: [m.reports_col_week(), m.reports_series_first_response(), m.reports_series_resolution()],
				body: weeks.map((w) => ({
					key: w.week_start,
					cells: [long(w.week_start), duration(w.first_response_seconds), duration(w.resolution_seconds)]
				}))
			}
		];
	});

	// "Show as table" per graph (FR-P1: every graph has a table view).
	let tables = $state<Record<string, boolean>>({});
</script>

<a class="back" href="/staff">{m.detail_back()}</a>
<header>
	<h1>{m.reports_heading()}</h1>
</header>

{#if !data.allowed}
	<Alert variant="error">{m.admin_forbidden()}</Alert>
{:else}
	<form
		class="filters"
		aria-label={m.reports_filters()}
		onsubmit={(e) => {
			e.preventDefault();
			apply(e.currentTarget);
		}}
	>
		<TextInput
			label={m.reports_from()}
			type="date"
			name="from"
			value={report?.period.from ?? ''}
			onchange={(e) => applyDates(e.currentTarget.form)}
		/>
		<TextInput
			label={m.reports_to()}
			type="date"
			name="to"
			value={report?.period.to ?? ''}
			onchange={(e) => applyDates(e.currentTarget.form)}
		/>
		<Select
			label={m.form_building()}
			name="building"
			options={[{ value: '', label: m.reports_all_buildings() }, ...data.buildingOptions]}
			value={page.url.searchParams.get('building') ?? ''}
		/>
		<Select
			label={m.detail_f_category()}
			name="category_id"
			options={[{ value: '', label: m.reports_all_categories() }, ...data.categories.map((c) => ({ value: String(c.id), label: c.name }))]}
			value={page.url.searchParams.get('category_id') ?? ''}
		/>
		<Button type="submit" variant="secondary">{m.reports_apply()}</Button>
	</form>

	{#if !report}
		<Alert variant="error">{m.reports_err_range()}</Alert>
	{:else}
		<div class={['content', busy && 'busy']} aria-busy={busy}>
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

			<WrittenSummary {report} />

			<div class="graphs">
				{#each graphs as g (g.id)}
					<Card>
						<div class="graph-head">
							<h2 id="g-{g.id}" tabindex="-1">{g.title}</h2>
							{#if !g.empty}
								<Button variant="secondary" aria-pressed={tables[g.id] ?? false} onclick={() => (tables[g.id] = !tables[g.id])}>
									{m.reports_show_table()}
								</Button>
							{/if}
						</div>
						{#if g.id === 'location'}
							<div class="drill">
								{#if building}
									<Button
										id="drill-back"
										variant="ghost"
										onclick={() => drillTo(floor ? { building: building.building } : {})}
									>
										{floor ? m.reports_back_floors({ building: building.building }) : m.reports_back_buildings()}
									</Button>
									<p class="muted">{building.building}{floor ? ` · ${floor.floor}` : ''}</p>
								{/if}
								{#if !floor && !g.empty}<p class="muted">{m.reports_drill_hint()}</p>{/if}
							</div>
						{/if}
						{#if g.empty}
							<p class="muted">{m.reports_empty()}</p>
						{:else if tables[g.id]}
							<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
							<div class="table-wrap" role="region" aria-labelledby="g-{g.id}" tabindex="0">
								<table>
									<caption class="visually-hidden">{g.title}</caption>
									<thead>
										<tr>{#each g.head as h, i (i)}<th scope="col">{h}</th>{/each}</tr>
									</thead>
									<tbody>
										{#each g.body as row (row.key)}
											<tr>
												<th scope="row">
													{#if row.drill}
														<button type="button" class="link" onclick={row.drill}>{row.cells[0]}</button>
													{:else}{row.cells[0]}{/if}
												</th>
												{#each row.cells.slice(1) as cell, i (i)}<td>{cell}</td>{/each}
											</tr>
										{/each}
									</tbody>
								</table>
							</div>
						{:else}
							<div class={['plot', g.rows !== undefined && 'rows']} style:--rows={g.rows}>
								<Chart config={g.config} label={g.title} />
							</div>
						{/if}
					</Card>
				{/each}
			</div>
		</div>
	{/if}
{/if}

<style>
	.back {
		display: inline-block;
		margin-bottom: var(--space-md);
		font: var(--font-label);
		color: var(--color-ink);
	}
	header {
		margin-bottom: var(--space-lg);
	}
	.muted {
		margin: 0;
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	/* One row above everything it scopes; wraps two per row on phones. */
	.filters {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: var(--space-sm);
		margin-bottom: var(--space-lg);
	}
	.filters > :global(.field) {
		flex: 1 1 130px;
		min-width: 0;
	}
	.filters + :global(.alert) {
		margin-bottom: var(--space-lg);
	}
	.content {
		display: flex;
		flex-direction: column;
		gap: var(--space-lg);
		transition: opacity var(--motion-fast);
	}
	/* Refetch keeps the frame: the previous render stays, dimmed, until the new data lands. */
	.busy {
		opacity: 0.5;
	}
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
	.graphs {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: var(--space-lg);
	}
	@media (min-width: 1024px) {
		.graphs {
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}
	}
	.graph-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-sm);
		margin-bottom: var(--space-md);
	}
	h2 {
		margin: 0;
		font: var(--font-title-md);
		color: var(--color-ink);
	}
	.drill {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-xs) var(--space-sm);
		margin-bottom: var(--space-sm);
	}
	/* Fixed heights include the axis band, so the card never scrolls inside itself. */
	.plot {
		position: relative;
		height: calc(var(--space-section) * 3);
	}
	.plot.rows {
		height: calc(var(--rows) * var(--space-xl) + var(--space-section));
	}
	.table-wrap {
		overflow-x: auto;
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
	}
	table {
		width: 100%;
		border-collapse: collapse;
		font: var(--font-body-sm);
	}
	th,
	td {
		padding: var(--space-xs) var(--space-sm);
		text-align: start;
	}
	thead th {
		background: var(--color-surface-soft);
		font: var(--font-label);
		color: var(--color-ink);
	}
	tbody th {
		font-weight: 400;
		color: var(--color-ink);
	}
	tbody tr {
		border-top: 1px solid var(--color-hairline);
	}
	td,
	thead th:not(:first-child) {
		text-align: end;
		font-variant-numeric: tabular-nums;
	}
	.link {
		min-height: var(--size-control);
		padding: 0;
		border: none;
		background: none;
		color: var(--color-ink);
		font: var(--font-label);
		text-align: start;
		text-decoration: underline;
		cursor: pointer;
	}
</style>
