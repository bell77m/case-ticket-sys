<!-- Dev-only gallery of base components (T1.08). Sample text here is not user-facing, so it is not translated. -->
<script lang="ts">
	import { onMount } from 'svelte';
	import Alert from '$lib/components/Alert.svelte';
	import Badge, { type Priority, type Status } from '$lib/components/Badge.svelte';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import Select from '$lib/components/Select.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import WrittenSummary from '$lib/components/WrittenSummary.svelte';
	import type { Report } from '$lib/api';
	import { showToast } from '$lib/components/toast-state.svelte';

	const statuses: Status[] = ['new', 'in_progress', 'waiting', 'resolved', 'closed'];
	const priorities: Priority[] = ['low', 'medium', 'high', 'urgent'];
	const buildings = [
		{ value: 'a', label: 'Building A' },
		{ value: 'b', label: 'Building B' }
	];
	let building = $state('');

	// Fixed report for the written summary (T3.02); e2e/summary.spec.ts checks these numbers. No first response
	// median, so the null branch shows. Only the fields the summary reads are filled.
	const sampleReport: Report = {
		period: { from: '2026-08-27', to: '2026-09-25', previous_from: '2026-07-28', previous_to: '2026-08-26' },
		cards: {
			open: { value: 312, previous: 298 },
			unassigned: { value: 27, previous: 31 },
			urgent_open: { value: 9, previous: 6 },
			new: { value: 1284, previous: 1190 },
			resolved: { value: 1201, previous: 1105 },
			first_response_median_seconds: { value: null, previous: 2460 },
			resolution_median_seconds: { value: 216000, previous: 190800 }
		},
		opened_resolved_daily: [],
		open_by_status: [],
		open_by_priority: [],
		by_category: [
			{ id: 3, name: 'Printer', count: 402 },
			{ id: 2, name: 'Computer', count: 385 },
			{ id: null, name: null, count: 120 }
		],
		by_location: [
			{ building: 'Building A', count: 731, floors: [] },
			{ building: 'Building B', count: 553, floors: [] }
		],
		workload: [],
		weekly_medians: []
	};
	let axeResult = $state('');

	// Open /dev/components?axe to run an axe-core accessibility check and print the result.
	onMount(async () => {
		if (!new URLSearchParams(location.search).has('axe')) return;
		const axe = (await import('axe-core')).default;
		const res = await axe.run(document, { runOnly: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'] });
		axeResult = JSON.stringify(
			res.violations.map((v) => ({ id: v.id, impact: v.impact, nodes: v.nodes.map((n) => [n.target, n.any.map((c) => c.message)]) })),
			null,
			2
		);
		axeResult = `violations: ${res.violations.length}\n${axeResult}`;
	});
</script>

<h1>Components</h1>

<div class="grid">
	<Card title="Buttons">
		<div class="row">
			<Button>Submit ticket</Button>
			<Button variant="secondary">Cancel</Button>
			<Button variant="ghost">Show as table</Button>
			<Button variant="danger">Delete ticket</Button>
			<Button disabled>Disabled</Button>
		</div>
	</Card>

	<Card title="Form fields">
		<div class="stack">
			<TextInput label="Guest name" helper="Up to 100 characters" />
			<TextInput label="Employee ID" error="Employee ID is required" />
			<Select label="Building" placeholder="Choose a building" options={buildings} bind:value={building} />
			<Select label="Floor" placeholder="Choose a building first" options={[]} disabled />
		</div>
	</Card>

	<Card title="Badges">
		<div class="row">
			{#each statuses as s (s)}<Badge kind="status" value={s} />{/each}
		</div>
		<div class="row">
			{#each priorities as p (p)}<Badge kind="priority" value={p} />{/each}
		</div>
	</Card>

	<Card title="Alerts and toast">
		<div class="stack">
			<Alert>Save this link. It is the only way to view your ticket.</Alert>
			<Alert variant="success">Ticket submitted.</Alert>
			<Alert variant="warning">Files over 100 MB are not accepted.</Alert>
			<Alert variant="error">Something went wrong. Try again.</Alert>
			<Button variant="ghost" onclick={() => showToast('Link copied')}>Show toast</Button>
		</div>
	</Card>

	<WrittenSummary report={sampleReport} />
</div>

{#if axeResult}<pre id="axe-result">{axeResult}</pre>{/if}

<style>
	h1 {
		margin-bottom: var(--space-lg);
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 360px), 1fr));
		gap: var(--space-lg);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-sm);
		margin-bottom: var(--space-sm);
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
	}
</style>
