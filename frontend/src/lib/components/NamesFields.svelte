<!-- One name in each app language (FR-I4). Layout: DESIGN.md "Names fields". -->
<script lang="ts">
	import type { Names } from '$lib/api';
	import { languageNames } from '$lib/LanguageSwitcher.svelte';
	import { m } from '$lib/paraglide/messages';
	import { locales } from '$lib/paraglide/runtime';
	import TextInput from './TextInput.svelte';

	type Props = {
		legend: string;
		value: Names;
		/** API field name; errors come as "<prefix>.<lang>", for example "name.th" or "building.my". */
		prefix: string;
		errors?: Record<string, string>;
	};
	let { legend, value = $bindable(), prefix, errors = {} }: Props = $props();

	const errorText: Record<string, () => string> = {
		required: m.err_required,
		too_long: m.err_too_long,
		invalid_characters: m.err_invalid_characters
	};
	const fieldError = (l: string) => {
		const code = errors[`${prefix}.${l}`];
		return code ? (errorText[code] ?? m.detail_err_invalid)() : undefined;
	};
</script>

<fieldset>
	<legend>{legend}</legend>
	<div class="grid">
		{#each locales as l (l)}
			<!-- lang: the per-language fonts and line heights apply while typing (DESIGN.md "Per-language rules"). -->
			<TextInput label={languageNames[l]} lang={l} bind:value={value[l]} error={fieldError(l)} maxlength={100} autocomplete="off" required />
		{/each}
	</div>
</fieldset>

<style>
	fieldset {
		margin: 0;
		padding: 0;
		border: none;
		min-width: 0;
	}
	legend {
		padding: 0;
		margin-bottom: var(--space-xs);
		font: var(--font-title-sm);
		color: var(--color-ink);
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 200px), 1fr));
		align-items: start;
		gap: var(--space-sm);
	}
</style>
