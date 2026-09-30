<!-- Reports dashboard (T3.03, FR-P1, FR-P2, FR-I5) and its PDF export (T3.05, FR-P4). Layout: DESIGN.md "Reports dashboard". -->
<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { goto, invalidate } from '$app/navigation';
	import { navigating, page } from '$app/state';
	import { ApiError, exportReport } from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import ReportCards from '$lib/components/ReportCards.svelte';
	import ReportGraph, { reportGraphs } from '$lib/components/ReportGraph.svelte';
	import Select from '$lib/components/Select.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import WrittenSummary from '$lib/components/WrittenSummary.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';
	import { coalesce, onTicketEvent } from '$lib/live.svelte';
	import { saveFile } from '$lib/print';

	let { data } = $props();
	const report = $derived(data.allowed ? data.report : null);
	const busy = $derived(navigating.to?.route.id === '/staff/reports');

	// Live (FR-P3): any ticket change refetches with the same filters. The previous render stays until the new data
	// lands; only filter changes dim it (busy), so a busy floor does not make the dashboard blink.
	onMount(() => onTicketEvent(coalesce(() => invalidate('app:reports'))));

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

	// Location drill-down: building → floor → line, by name (names are in the viewer's language).
	let drill = $state<{ building?: string; floor?: string }>({});
	const building = $derived(report?.by_location.find((b) => b.building === drill.building));
	const floor = $derived(building?.floors.find((f) => f.floor === drill.floor));
	async function drillTo(next: { building?: string; floor?: string }) {
		drill = next;
		await tick();
		(document.getElementById('drill-back') ?? document.getElementById('g-location'))?.focus();
	}

	const graphs = $derived(report ? reportGraphs(report, { building, floor, open: drillTo }) : []);

	// The filters every card link carries to the queue, with the time zone the report used (FR-P1).
	const cardScope = $derived.by(() => {
		const p = new URLSearchParams({ tz: Intl.DateTimeFormat().resolvedOptions().timeZone });
		for (const key of ['building', 'category_id']) {
			const v = page.url.searchParams.get(key);
			if (v) p.set(key, v);
		}
		return p.toString();
	});

	// Export PDF (FR-P4): the view on show, in the viewer's language (FR-I6). Gotenberg takes a few seconds.
	let exporting = $state(false);
	let exportError = $state('');
	async function exportPdf() {
		if (!report) return;
		exporting = true;
		exportError = '';
		try {
			const { name, blob } = await exportReport({
				from: report.period.from,
				to: report.period.to,
				tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
				building: page.url.searchParams.get('building') ?? '',
				category_id: page.url.searchParams.get('category_id') ?? '',
				lang: getLocale()
			});
			saveFile(name, blob);
		} catch (err) {
			exportError = !(err instanceof ApiError)
				? m.err_network()
				: err.code === 'pdf.unavailable'
					? m.reports_err_pdf_unavailable()
					: err.code === 'validation'
						? m.reports_err_range()
						: err.status === 401
							? m.detail_err_session()
							: err.status === 403
								? m.detail_err_forbidden()
								: m.reports_err_pdf_failed();
		} finally {
			exporting = false;
		}
	}
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
		{#if report}
			<Button variant="secondary" disabled={exporting} onclick={exportPdf}>
				{exporting ? m.reports_exporting() : m.reports_export()}
			</Button>
		{/if}
	</form>

	{#if exportError}
		<Alert variant="error">{exportError}</Alert>
	{/if}
	{#if !report}
		<Alert variant="error">{m.reports_err_range()}</Alert>
	{:else}
		<div class={['content', busy && 'busy']} aria-busy={busy}>
			<ReportCards {report} scope={cardScope} />

			<WrittenSummary {report} />

			<div class="graphs">
				{#each graphs as g (g.id)}
					<ReportGraph graph={g}>
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
					</ReportGraph>
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
	.filters ~ :global(.alert) {
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
	.drill {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-xs) var(--space-sm);
		margin-bottom: var(--space-sm);
	}
</style>
