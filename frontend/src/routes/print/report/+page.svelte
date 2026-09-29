<!-- Report print page (T3.05, FR-P4, FR-I6): what Gotenberg prints to PDF. Layout: DESIGN.md "Report print page". -->
<!-- No staff chrome (routes/+layout.svelte leaves it out on /print/) and no session: a one-time token in the URL fragment. -->
<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { getPrintReport, type PrintReport } from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import ReportCards from '$lib/components/ReportCards.svelte';
	import ReportGraph, { reportGraphs } from '$lib/components/ReportGraph.svelte';
	import WrittenSummary from '$lib/components/WrittenSummary.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';
	import { failPrint, fontsLoaded, loadPrint } from '$lib/print';

	let report = $state<PrintReport>();
	let error = $state('');
	/** Charts wait for the fonts: canvas text drawn before they load stays in a fallback face. */
	let charts = $state(false);

	// Read once the report (and so the language) is in; dates as the dashboard shows them (FR-I5).
	const period = $derived.by(() => {
		if (!report) return '';
		const f = new Intl.DateTimeFormat(getLocale(), { dateStyle: 'medium', timeZone: 'UTC', calendar: 'gregory' });
		return f.formatRange(new Date(report.period.from), new Date(report.period.to));
	});

	/** Shows the message, then fails the print (see failPrint). */
	function fail(message: string) {
		if (error) return;
		error = message;
		failPrint();
	}

	onMount(async () => {
		const data = await loadPrint(getPrintReport);
		if (typeof data === 'string') return fail(data === 'expired' ? m.print_expired() : m.print_failed());
		try {
			report = data;
			await tick();
			await fontsLoaded();
			charts = true;
			await tick();
			await new Promise(requestAnimationFrame); // charts draw at once when mounted (no animation); one frame to paint
			await document.fonts.ready; // table text added with the charts
			if (!error) window.printReady = true; // Gotenberg prints now
		} catch {
			fail(m.print_failed());
		}
	});
</script>

<svelte:head>
	<title>{report ? m.print_title() : m.app_title()}</title>
</svelte:head>

<div class="sheet">
	{#if error}
		<Alert variant="error">{error}</Alert>
	{:else if report}
		<!-- A render error (a chart that throws) fails the print too. -->
		<svelte:boundary onerror={() => fail(m.print_failed())}>
			<header>
				<h1>{m.print_title()}</h1>
				<p>{m.print_period({ period })}</p>
				<p>{m.print_building({ building: report.filters.building ?? m.reports_all_buildings() })}</p>
				<p>{m.print_category({ category: report.filters.category ?? m.reports_all_categories() })}</p>
			</header>
			<ReportCards {report} links={false} />
			<WrittenSummary {report} />
			{#if charts}
				{#each reportGraphs(report) as g (g.id)}
					<ReportGraph graph={g} print />
				{/each}
			{/if}
		</svelte:boundary>
	{/if}
</div>

<style>
	/* The backend asks Gotenberg for A4 with 0.4in margins too; these win (preferCssPageSize). */
	@page {
		size: A4;
		margin: 0.4in;
	}
	/* Fixed width, narrower than A4 inside its margins: charts are drawn at this size on screen and never resize in print. */
	.sheet {
		display: flex;
		flex-direction: column;
		gap: var(--space-lg);
		width: 180mm;
		margin: 0 auto;
		color: var(--color-ink);
	}
	header p {
		margin: var(--space-xs) 0 0;
		font: var(--font-body-md);
		color: var(--color-body);
	}
	/* Seven cards in two rows of four and three. */
	.sheet :global(.cards) {
		grid-template-columns: repeat(4, minmax(0, 1fr));
	}
</style>
