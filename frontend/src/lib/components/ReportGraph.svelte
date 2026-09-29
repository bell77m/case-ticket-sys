<!-- One graph card of the reports dashboard and its print page (FR-P1, FR-P4). Layout: DESIGN.md "Reports dashboard". -->
<script lang="ts" module>
	import type { ChartConfiguration, TooltipItem } from 'chart.js';
	import type { Report } from '$lib/api';
	import { labels } from '$lib/components/Badge.svelte';
	import { cssVar } from '$lib/components/Chart.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';
	import { formatDuration } from '$lib/report-summary';

	type Row = { key: string; cells: string[]; drill?: () => void };
	export type Graph = {
		id: string;
		title: string;
		empty: boolean;
		config: ChartConfiguration;
		/** Bars per row for horizontal charts: the plot grows with them. */
		rows?: number;
		head: string[];
		body: Row[];
	};
	type Building = Report['by_location'][number];
	/** Location drill-down on show; `open` goes a level down (the dashboard only). */
	export type Drill = {
		building?: Building;
		floor?: Building['floors'][number];
		open?: (next: { building: string; floor?: string }) => void;
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

	/** The 7 graphs of REQUIREMENTS §5, each with its table, in the viewer's language (FR-I5). */
	export function reportGraphs(r: Report, { building, floor, open }: Drill = {}): Graph[] {
		// Intl in the viewer's language. Gregorian calendar in every language, as in the queue.
		const locale = getLocale();
		const num = new Intl.NumberFormat(locale).format;
		const hours = new Intl.NumberFormat(locale, { style: 'unit', unit: 'hour', unitDisplay: 'short', maximumSignificantDigits: 3 }).format;
		// API dates are plain YYYY-MM-DD, read as UTC midnight, so they are formatted in UTC.
		const dayShort = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', timeZone: 'UTC', calendar: 'gregory' });
		const dayLong = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC', calendar: 'gregory' });
		const short = (d: string) => dayShort.format(new Date(d));
		const long = (d: string) => dayLong.format(new Date(d));
		const duration = (s: number | null) => (s === null ? m.reports_no_data() : formatDuration(s, locale));

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
						open: open && ((i: number) => open({ building: building.building, floor: building.floors[i].floor }))
					}
				: {
						head: m.form_building(),
						items: r.by_location.map((b) => ({ name: b.building, count: b.count })),
						open: open && ((i: number) => open({ building: r.by_location[i].building }))
					};

		const down = level.open;
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
					drill: down && (() => down(i))
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
	}
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import Chart from '$lib/components/Chart.svelte';

	/** print: the chart and its table, one under the other, no toggle (the PDF page). `children` sits above the plot. */
	let { graph: g, print = false, children }: { graph: Graph; print?: boolean; children?: Snippet } = $props();

	// "Show as table" (FR-P1: every graph has a table view).
	let table = $state(false);
</script>

<Card>
	<div class="graph-head">
		<h2 id="g-{g.id}" tabindex="-1">{g.title}</h2>
		{#if !g.empty && !print}
			<Button variant="secondary" aria-pressed={table} onclick={() => (table = !table)}>{m.reports_show_table()}</Button>
		{/if}
	</div>
	{@render children?.()}
	{#if g.empty}
		<p class="muted">{m.reports_empty()}</p>
	{:else}
		{#if print || !table}
			<div class={['plot', g.rows !== undefined && 'rows']} style:--rows={g.rows}>
				<Chart config={g.config} label={g.title} {print} />
			</div>
		{/if}
		{#if print || table}
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
		{/if}
	{/if}
</Card>

<style>
	.graph-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-sm);
		margin-bottom: var(--space-md);
		break-after: avoid;
	}
	h2 {
		margin: 0;
		font: var(--font-title-md);
		color: var(--color-ink);
	}
	.muted {
		margin: 0;
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	/* Fixed heights include the axis band, so the card never scrolls inside itself. */
	.plot {
		position: relative;
		height: calc(var(--space-section) * 3);
		break-inside: avoid;
	}
	.plot.rows {
		height: calc(var(--rows) * var(--space-xl) + var(--space-section));
	}
	.plot + .table-wrap {
		margin-top: var(--space-md);
	}
	.table-wrap {
		overflow-x: auto;
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
	}
	/* A scroll box never splits across pages: in print, long tables must flow on. */
	@media print {
		.table-wrap {
			overflow: visible;
		}
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
	tr {
		break-inside: avoid;
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
