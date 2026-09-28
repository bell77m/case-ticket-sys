<script lang="ts">
	import type { Snippet } from 'svelte';

	let { title, children }: { title?: string; children: Snippet } = $props();
</script>

<section class="card">
	{#if title}<h2>{title}</h2>{/if}
	{@render children()}
</section>

<style>
	.card {
		padding: var(--space-lg);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
		background: var(--color-canvas);
		box-shadow: var(--shadow-sm);
	}
	h2 {
		margin: 0 0 var(--space-md);
		font: var(--font-title-md);
		color: var(--color-ink);
	}
</style>
