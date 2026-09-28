<!-- Staff sign-in (T2.15, FR-R1): username and password. The API answers every failure the same way (FR-A2). -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { ApiError, login } from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import { m } from '$lib/paraglide/messages';

	let username = $state('');
	let password = $state('');
	let errors = $state<{ username?: string; password?: string }>({});
	let error = $state('');
	let busy = $state(false);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		errors = {};
		if (!username.trim()) errors.username = m.err_required();
		if (!password) errors.password = m.err_required();
		if (errors.username || errors.password) return;
		busy = true;
		try {
			const res = await login(username, password);
			await goto(res.must_change_password ? '/staff/password' : '/staff');
		} catch (err) {
			password = '';
			if (!(err instanceof ApiError)) error = m.err_network();
			else if (err.code === 'auth.invalid') error = m.login_err_invalid();
			else if (err.code === 'auth.too_many_attempts') error = m.login_err_too_many();
			else error = m.login_err_failed();
		} finally {
			busy = false;
		}
	}
</script>

<form onsubmit={submit} novalidate>
	<h1>{m.login_heading()}</h1>
	<p>{m.login_intro()}</p>
	{#if error}<Alert variant="error">{error}</Alert>{/if}
	<TextInput
		label={m.login_username()}
		bind:value={username}
		error={errors.username}
		autocomplete="username"
		autocapitalize="none"
		spellcheck="false"
		maxlength={64}
		required
	/>
	<TextInput
		label={m.login_password()}
		type="password"
		bind:value={password}
		error={errors.password}
		autocomplete="current-password"
		required
	/>
	<Button type="submit" disabled={busy}>{m.login_button()}</Button>
</form>

<style>
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
	p {
		margin: 0;
		color: var(--color-muted);
	}
</style>
