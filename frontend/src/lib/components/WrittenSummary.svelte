<!-- Written summary card of the reports dashboard (FR-P1, DESIGN.md "Reports"). -->
<script lang="ts">
	import type { Report } from '$lib/api';
	import Card from '$lib/components/Card.svelte';
	import { m } from '$lib/paraglide/messages';
	import { writtenSummary } from '$lib/report-summary';

	let { report }: { report: Report } = $props();
	const sentences = $derived(writtenSummary(report));
</script>

<Card title={m.summary_title()}>
	<ul class="summary">
		{#each sentences as s, i (i)}<li>{s}</li>{/each}
	</ul>
</Card>

<style>
	.summary {
		display: flex;
		flex-direction: column;
		gap: var(--space-xs);
		margin: 0;
		padding-inline-start: var(--space-lg);
		color: var(--color-body);
	}
</style>
