<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { HTMLButtonAttributes } from 'svelte/elements';

	type Props = HTMLButtonAttributes & {
		variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
		/** Renders a link that looks like a button, for navigation. */
		href?: string;
		children: Snippet;
	};
	let { variant = 'primary', type = 'button', href, children, ...rest }: Props = $props();
</script>

{#if href}
	<a class="btn {variant}" {href}>{@render children()}</a>
{:else}
	<button class="btn {variant}" {type} {...rest}>{@render children()}</button>
{/if}

<style>
	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-xs);
		min-height: var(--size-control);
		padding: var(--space-sm) var(--space-lg);
		border: 1px solid transparent;
		border-radius: var(--radius-md);
		font: var(--font-button);
		text-decoration: none;
		cursor: pointer;
		transition: background-color var(--motion-fast);
	}
	button:disabled {
		cursor: not-allowed;
	}
	.primary {
		background: var(--color-primary);
		color: var(--color-on-primary);
	}
	.primary:active {
		background: var(--color-primary-active);
	}
	.primary:disabled {
		background: var(--color-primary-disabled);
		color: var(--color-muted);
	}
	.secondary {
		background: var(--color-canvas);
		color: var(--color-ink);
		border-color: var(--color-hairline);
		box-shadow: var(--shadow-sm);
	}
	.ghost {
		min-height: auto;
		padding: var(--space-xs) var(--space-md);
		background: var(--color-surface-card);
		color: var(--color-ink);
	}
	.danger {
		background: var(--color-error-strong);
		color: var(--color-on-primary);
	}
	.secondary:disabled,
	.ghost:disabled,
	.danger:disabled {
		opacity: 0.5;
	}
</style>
