<script lang="ts">
	import type { Snippet } from 'svelte';

	type Variant = 'info' | 'success' | 'warning' | 'error';
	let { variant = 'info', children }: { variant?: Variant; children: Snippet } = $props();

	// Icon so color is never the only signal (DESIGN.md "Feedback").
	const icons: Record<Variant, string> = { info: 'ℹ', success: '✓', warning: '!', error: '✕' };
</script>

<div class="alert {variant}" role={variant === 'error' ? 'alert' : 'status'}>
	<span class="icon" aria-hidden="true">{icons[variant]}</span>
	<div>{@render children()}</div>
</div>

<style>
	.alert {
		display: flex;
		align-items: flex-start;
		gap: var(--space-sm);
		padding: var(--space-sm) var(--space-md);
		border-radius: var(--radius-lg);
		font: var(--font-body-sm);
		color: var(--color-ink);
	}
	.icon {
		flex: none;
		display: grid;
		place-items: center;
		width: var(--space-lg);
		height: var(--space-lg);
		border-radius: var(--radius-full);
		background: var(--color-canvas);
		font: var(--font-label);
		line-height: 1;
	}
	.info {
		background: var(--color-accent-tint);
	}
	.success {
		background: var(--color-success-tint);
	}
	.warning {
		background: var(--color-warning-tint);
	}
	.error {
		background: var(--color-error-tint);
	}
</style>
