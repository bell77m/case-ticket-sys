<!-- Change password (T2.15, FR-A8, FR-A9). Forced after a temporary password: staff/+layout.ts sends staff here until
     it is done, and the API refuses other staff calls meanwhile. -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { ApiError, changePassword } from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import { showToast } from '$lib/components/toast-state.svelte';
	import { m } from '$lib/paraglide/messages';

	let { data } = $props();

	let current = $state('');
	let next = $state('');
	let repeat = $state('');
	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	let busy = $state(false);

	// API field codes, translated here (CLAUDE.md "i18n").
	const fieldText: Record<string, () => string> = {
		required: m.err_required,
		wrong: m.password_err_wrong,
		too_short: m.password_err_too_short,
		too_long: m.password_err_too_long,
		same: m.password_err_same,
		mismatch: m.password_err_mismatch
	};
	const fieldError = (f: string) => (errors[f] ? (fieldText[errors[f]] ?? m.err_required)() : undefined);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		formError = '';
		errors = {};
		if (!current) errors.current_password = 'required';
		if (!next) errors.new_password = 'required';
		else if (next !== repeat) errors.repeat = 'mismatch';
		if (Object.keys(errors).length) return;
		busy = true;
		try {
			await changePassword(current, next);
			showToast(m.password_changed());
			await goto('/staff', { invalidateAll: true });
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') errors = err.fields;
			else if (err instanceof ApiError && err.code === 'auth.too_many_attempts') formError = m.login_err_too_many();
			else if (err instanceof ApiError && err.status === 401) await goto('/login');
			else formError = err instanceof ApiError ? m.detail_err_failed() : m.err_network();
		} finally {
			busy = false;
		}
	}
</script>

{#if !data.me.must_change_password}<a class="back" href="/staff">{m.detail_back()}</a>{/if}

<form onsubmit={submit} novalidate>
	<h1>{m.password_heading()}</h1>
	{#if data.me.must_change_password}<Alert>{m.password_forced()}</Alert>{/if}
	{#if formError}<Alert variant="error">{formError}</Alert>{/if}
	<!-- Lets password managers file the new password under the right account. -->
	<input type="text" autocomplete="username" value={data.me.username} hidden readonly />
	<TextInput
		label={m.password_current()}
		type="password"
		bind:value={current}
		error={fieldError('current_password')}
		autocomplete="current-password"
		required
	/>
	<TextInput
		label={m.password_new()}
		type="password"
		bind:value={next}
		error={fieldError('new_password')}
		helper={m.password_new_helper()}
		autocomplete="new-password"
		required
	/>
	<TextInput
		label={m.password_repeat()}
		type="password"
		bind:value={repeat}
		error={fieldError('repeat')}
		autocomplete="new-password"
		required
	/>
	<Button type="submit" disabled={busy}>{m.password_save()}</Button>
</form>

<style>
	.back {
		display: inline-block;
		margin-bottom: var(--space-md);
		font: var(--font-label);
		color: var(--color-ink);
	}
	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
		max-width: 440px;
		margin: var(--space-xl) auto 0;
		padding: var(--space-xl);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-xl);
		box-shadow: var(--shadow-md);
	}
	h1 {
		margin: 0;
	}
</style>
