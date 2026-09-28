<script lang="ts">
	import { toast } from './toast-state.svelte';
</script>

<!-- Always in the DOM so screen readers announce new messages. -->
<div class="region" aria-live="polite">
	{#if toast.message}<div class="toast">{toast.message}</div>{/if}
</div>

<style>
	.region {
		position: fixed;
		bottom: var(--space-lg);
		left: 50%;
		transform: translateX(-50%);
		pointer-events: none;
	}
	.toast {
		padding: var(--space-sm) var(--space-lg);
		border-radius: var(--radius-pill);
		background: var(--color-surface-dark);
		color: var(--color-on-dark);
		font: var(--font-body-sm);
		box-shadow: var(--shadow-md);
	}
</style>
