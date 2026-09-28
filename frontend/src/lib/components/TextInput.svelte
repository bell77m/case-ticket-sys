<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';

	type Props = HTMLInputAttributes & {
		label: string;
		helper?: string;
		error?: string;
		value?: string;
	};
	let { label, helper, error, value = $bindable(''), id, ...rest }: Props = $props();

	const uid = $props.id();
	const inputId = $derived(id ?? `input-${uid}`);
	const describedBy = $derived(error ? `${inputId}-error` : helper ? `${inputId}-helper` : undefined);
</script>

<div class="field">
	<label for={inputId}>{label}</label>
	<input
		id={inputId}
		bind:value
		aria-invalid={error ? 'true' : undefined}
		aria-describedby={describedBy}
		{...rest}
	/>
	{#if error}
		<p id="{inputId}-error" class="error" aria-live="polite">{error}</p>
	{:else if helper}
		<p id="{inputId}-helper" class="helper">{helper}</p>
	{/if}
</div>

<style>
	.field {
		display: flex;
		flex-direction: column;
		gap: var(--space-xxs);
	}
	label {
		font: var(--font-label);
		color: var(--color-ink);
	}
	input {
		min-height: var(--size-control);
		padding: var(--space-sm);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-md);
		background: var(--color-canvas);
		color: var(--color-ink);
		font: var(--font-body-md);
		box-shadow: var(--shadow-sm);
		transition: box-shadow var(--motion-fast);
	}
	input:focus {
		outline: none;
		border-color: var(--color-focus);
		box-shadow: 0 0 0 var(--space-xxs) var(--color-focus-ring);
	}
	input[aria-invalid='true'] {
		border-color: var(--color-error);
	}
	input::placeholder {
		color: var(--color-muted);
	}
	.helper,
	.error {
		margin: 0;
		font: var(--font-body-sm);
	}
	.helper {
		color: var(--color-muted);
	}
	.error {
		color: var(--color-error-strong);
	}
</style>
