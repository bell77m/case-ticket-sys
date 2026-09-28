<script lang="ts">
	import type { HTMLSelectAttributes } from 'svelte/elements';

	type Option = { value: string; label: string };
	type Props = HTMLSelectAttributes & {
		label: string;
		/** Screen readers only: for a select in a table cell, where the column header is the visible label. */
		hideLabel?: boolean;
		options: Option[];
		placeholder?: string;
		error?: string;
		value?: string;
	};
	let { label, hideLabel = false, options, placeholder, error, value = $bindable(''), id, ...rest }: Props = $props();

	const uid = $props.id();
	const selectId = $derived(id ?? `select-${uid}`);
</script>

<div class="field">
	<label for={selectId} class:visually-hidden={hideLabel}>{label}</label>
	<select
		id={selectId}
		bind:value
		aria-invalid={error ? 'true' : undefined}
		aria-describedby={error ? `${selectId}-error` : undefined}
		{...rest}
	>
		{#if placeholder}
			<option value="" disabled>{placeholder}</option>
		{/if}
		{#each options as option (option.value)}
			<option value={option.value}>{option.label}</option>
		{/each}
	</select>
	{#if error}
		<p id="{selectId}-error" class="error" aria-live="polite">{error}</p>
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
	select {
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
	select:focus {
		outline: none;
		border-color: var(--color-focus);
		box-shadow: 0 0 0 var(--space-xxs) var(--color-focus-ring);
	}
	select[aria-invalid='true'] {
		border-color: var(--color-error);
	}
	select:disabled {
		background: var(--color-surface-soft);
		color: var(--color-muted-soft);
		box-shadow: none;
	}
	.error {
		margin: 0;
		font: var(--font-body-sm);
		color: var(--color-error-strong);
	}
</style>
