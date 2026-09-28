<!-- Staff accounts (T2.10, FR-A1 to FR-A5). Layout: DESIGN.md "Staff accounts". -->
<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { ApiError, createStaffAccount, resetStaffPassword, updateStaffAccount, type StaffAccount } from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import Select from '$lib/components/Select.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import { showToast } from '$lib/components/toast-state.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';

	let { data } = $props();

	// Display only: the API checks each permission again (FR-R3).
	const can = (p: string) => data.me.permissions.includes(p);
	// FR-A4: only Root Admin (role.manage) sees the Root Admin role and the controls on Root Admin rows.
	const rootRoles = $derived(new Set(data.roles.filter((r) => r.root).map((r) => r.id)));
	const editable = (a: StaffAccount) => can('role.manage') || !rootRoles.has(a.role.id);
	const roleOptions = $derived(
		data.roles.filter((r) => !r.root || can('role.manage')).map((r) => ({ value: String(r.id), label: r.name }))
	);

	// API error codes, translated here (CLAUDE.md "i18n"). Field codes cover name, username, password and role_id.
	const errorText: Record<string, () => string> = {
		'staff.root_admin_only': m.staff_err_root_admin_only,
		'staff.last_root_admin': m.staff_err_last_root_admin,
		'staff.not_found': m.staff_err_not_found,
		'auth.forbidden': m.detail_err_forbidden,
		'auth.required': m.detail_err_session,
		validation: m.detail_err_invalid
	};
	const fieldText: Record<string, () => string> = {
		required: m.err_required,
		too_long: m.err_too_long,
		too_short: m.password_err_too_short,
		invalid_characters: m.err_invalid_characters,
		not_found: m.staff_err_role_not_found
	};
	// Codes whose message depends on the field.
	const perField: Record<string, Record<string, () => string>> = {
		username: { invalid: m.staff_err_username_invalid, taken: m.staff_err_username_taken },
		password: { too_long: m.password_err_too_long }
	};
	const codeText = (field: string, code: string) => (perField[field]?.[code] ?? fieldText[code] ?? m.err_required)();
	function describeError(err: unknown) {
		if (!(err instanceof ApiError)) return m.err_network();
		if (err.fields.role_id) return codeText('role_id', err.fields.role_id);
		if (err.fields.password) return codeText('password', err.fields.password);
		return (errorText[err.code] ?? m.detail_err_failed)();
	}

	let busy = $state(false);

	// Create (staff.create, Root Admin only: FR-A1). The password is temporary (FR-A8).
	let name = $state('');
	let username = $state('');
	let roleId = $state('');
	let password = $state('');
	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	const fieldError = (f: string) => (errors[f] ? codeText(f, errors[f]) : undefined);

	async function create(event: SubmitEvent) {
		event.preventDefault();
		formError = '';
		errors = {};
		if (!name.trim()) errors.name = 'required';
		if (!username.trim()) errors.username = 'required';
		if (!roleId) errors.role_id = 'required';
		if (!password) errors.password = 'required';
		if (Object.keys(errors).length) return;
		busy = true;
		try {
			await createStaffAccount({ name, username, role_id: Number(roleId), password });
			name = username = roleId = password = '';
			showToast(m.staff_added());
			await invalidateAll();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') errors = err.fields;
			else if (err instanceof ApiError && err.code === 'staff.username_taken') errors = { username: 'taken' };
			else formError = describeError(err);
		} finally {
			busy = false;
		}
	}

	// Per row: the role picked but not saved yet (never saved on change: .claude/rules/frontend.md).
	let choice = $state<Record<number, string>>({});
	let rowError = $state.raw<{ id: number; text: string } | null>(null);

	async function update(a: StaffAccount, patch: { role_id?: number; is_active?: boolean }, done: string) {
		busy = true;
		rowError = null;
		try {
			await updateStaffAccount(a.id, patch);
			delete choice[a.id];
			showToast(done);
		} catch (err) {
			rowError = { id: a.id, text: describeError(err) };
		}
		await invalidateAll();
		busy = false;
	}

	function saveRole(event: SubmitEvent, a: StaffAccount) {
		event.preventDefault();
		const id = Number(choice[a.id] ?? a.role.id);
		if (id === a.role.id) return showToast(m.detail_no_changes());
		update(a, { role_id: id }, m.detail_saved());
	}

	// Deactivate and reactivate ask first, in a native modal dialog (focus, Esc).
	let dialog: HTMLDialogElement;
	let pending = $state.raw<StaffAccount | null>(null);
	function ask(a: StaffAccount) {
		pending = a;
		dialog.showModal();
	}
	function confirmActive() {
		dialog.close();
		if (!pending) return;
		kept = pending.id;
		update(pending, { is_active: !pending.is_active }, pending.is_active ? m.staff_deactivated() : m.staff_reactivated());
	}

	// Reset password (Root Admin only: FR-A9), in its own modal dialog.
	let resetDialog: HTMLDialogElement;
	let resetting = $state.raw<StaffAccount | null>(null);
	let resetPassword = $state('');
	let resetError = $state<string>();
	function askReset(a: StaffAccount) {
		resetting = a;
		resetPassword = '';
		resetError = undefined;
		resetDialog.showModal();
	}
	async function confirmReset(event: SubmitEvent) {
		event.preventDefault();
		if (!resetting) return;
		if (!resetPassword) return (resetError = m.err_required());
		busy = true;
		try {
			await resetStaffPassword(resetting.id, resetPassword);
			resetDialog.close();
			kept = resetting.id;
			showToast(m.staff_reset_done());
		} catch (err) {
			resetError = describeError(err);
		} finally {
			busy = false;
		}
	}

	// Deactivated accounts stay out of the list until asked for; they pile up over the years. A row changed on this
	// visit stays, so focus does not drop to the page when its button goes away.
	let showInactive = $state(false);
	let kept = $state<number>();
	const active = $derived(data.accounts?.filter((a) => a.is_active).length ?? 0);
	const inactive = $derived((data.accounts?.length ?? 0) - active);
	const shown = $derived(data.accounts?.filter((a) => a.is_active || showInactive || a.id === kept) ?? []);

	// Gregorian calendar in every language, including Thai (open question in docs/PLAN.md).
	const dateFmt = new Intl.DateTimeFormat(getLocale(), { dateStyle: 'medium', calendar: 'gregory' });
	const fmt = (iso: string) => dateFmt.format(new Date(iso));
	const count = (n: number) => new Intl.NumberFormat(getLocale()).format(n);
</script>

{#snippet roleForm(a: StaffAccount, label: string, hideLabel: boolean)}
	<form class="inline" onsubmit={(e) => saveRole(e, a)}>
		<Select
			{label}
			{hideLabel}
			options={roleOptions}
			bind:value={() => choice[a.id] ?? String(a.role.id), (v) => (choice[a.id] = v)}
		/>
		<Button type="submit" variant="secondary" disabled={busy}>{m.admin_save()}</Button>
	</form>
{/snippet}

{#snippet stateBadge(a: StaffAccount)}
	<span class={['state', a.is_active && 'active']}>
		<span class="dot" aria-hidden="true"></span>{a.is_active ? m.staff_active() : m.staff_inactive()}
	</span>
{/snippet}

{#snippet toggle(a: StaffAccount)}
	<Button variant="secondary" disabled={busy} onclick={() => ask(a)}>
		{a.is_active ? m.staff_deactivate() : m.staff_reactivate()}
	</Button>
{/snippet}

{#snippet resetButton(a: StaffAccount)}
	{#if can('staff.create')}
		<Button variant="secondary" disabled={busy} onclick={() => askReset(a)}>{m.staff_reset_password()}</Button>
	{/if}
{/snippet}

<a class="back" href="/staff">{m.detail_back()}</a>

<header>
	<h1>{m.staff_heading()}</h1>
	{#if data.accounts}
		<span class="muted">{active === 1 ? m.staff_total_one() : m.staff_total({ count: count(active) })}</span>
	{/if}
</header>

{#if !data.accounts}
	<Alert variant="error">{m.admin_forbidden()}</Alert>
{:else}
	{#if can('staff.create')}
		<div class="add">
			<Card title={m.staff_add_title()}>
				<form class="create" onsubmit={create} novalidate>
					<div class="fields">
						<TextInput label={m.staff_name()} bind:value={name} error={fieldError('name')} maxlength={100} autocomplete="off" required />
						<TextInput
							label={m.staff_username()}
							helper={m.staff_username_helper()}
							bind:value={username}
							error={fieldError('username')}
							maxlength={64}
							autocomplete="off"
							autocapitalize="none"
							spellcheck="false"
							required
						/>
						<Select
							label={m.staff_role()}
							placeholder={m.staff_role_placeholder()}
							options={roleOptions}
							bind:value={roleId}
							error={fieldError('role_id')}
							required
						/>
						<!-- Shown in clear on purpose: the Root Admin passes it on (FR-A8 makes the person replace it). -->
						<TextInput
							label={m.staff_temp_password()}
							helper={m.staff_temp_password_helper()}
							bind:value={password}
							error={fieldError('password')}
							autocomplete="off"
							spellcheck="false"
							required
						/>
					</div>
					{#if formError}<Alert variant="error">{formError}</Alert>{/if}
					<div><Button type="submit" disabled={busy}>{m.staff_add()}</Button></div>
				</form>
			</Card>
		</div>
	{/if}

	{#if inactive}
		<label class="show-inactive">
			<input type="checkbox" bind:checked={showInactive} />
			{m.staff_show_inactive({ count: count(inactive) })}
		</label>
	{/if}

	<!-- From 1024px: a table. Narrower: the same accounts as stacked cards (DESIGN.md "Staff queue"). -->
	<table>
		<thead>
			<tr>
				<th scope="col">{m.staff_name()}</th>
				<th scope="col">{m.staff_username()}</th>
				<th scope="col">{m.staff_role()}</th>
				<th scope="col">{m.queue_col_status()}</th>
				<th scope="col">{m.queue_col_created()}</th>
			</tr>
		</thead>
		<tbody>
			{#each shown as a (a.id)}
				<tr>
					<td class="who">{a.name}</td>
					<td class="username">{a.username}</td>
					<td>
						{#if editable(a)}{@render roleForm(a, m.staff_role_for({ name: a.name }), true)}{:else}{a.role.name}{/if}
					</td>
					<td>
						<div class="access">
							{@render stateBadge(a)}
							{#if editable(a)}{@render toggle(a)}{/if}
							{@render resetButton(a)}
						</div>
					</td>
					<td class="nowrap">{fmt(a.created_at)}</td>
				</tr>
				{#if rowError?.id === a.id}
					<tr><td colspan="5" class="row-error"><Alert variant="error">{rowError.text}</Alert></td></tr>
				{/if}
			{/each}
		</tbody>
	</table>

	<ul class="cards">
		{#each shown as a (a.id)}
			<li>
				<div class="top">
					<span class="who">{a.name}</span>
					{@render stateBadge(a)}
				</div>
				<div class="muted username">{a.username}</div>
				<div class="muted">{m.staff_added_on({ date: fmt(a.created_at) })}</div>
				{#if editable(a)}
					{@render roleForm(a, m.staff_role(), false)}
					<div class="access">{@render toggle(a)}{@render resetButton(a)}</div>
				{:else}
					<div>{a.role.name}</div>
				{/if}
				{#if rowError?.id === a.id}<Alert variant="error">{rowError.text}</Alert>{/if}
			</li>
		{/each}
	</ul>
{/if}

<dialog bind:this={dialog} aria-labelledby="confirm-title">
	{#if pending}
		<h2 id="confirm-title">
			{pending.is_active ? m.staff_deactivate_title({ name: pending.name }) : m.staff_reactivate_title({ name: pending.name })}
		</h2>
		<p>{pending.is_active ? m.staff_deactivate_text() : m.staff_reactivate_text()}</p>
		<form method="dialog">
			<Button type="submit" variant="secondary">{m.admin_cancel()}</Button>
			<Button variant={pending.is_active ? 'danger' : 'primary'} onclick={confirmActive}>
				{pending.is_active ? m.staff_deactivate() : m.staff_reactivate()}
			</Button>
		</form>
	{/if}
</dialog>

<dialog bind:this={resetDialog} aria-labelledby="reset-title">
	{#if resetting}
		<h2 id="reset-title">{m.staff_reset_title({ name: resetting.name })}</h2>
		<p>{m.staff_reset_text()}</p>
		<form class="reset" onsubmit={confirmReset} novalidate>
			<TextInput
				label={m.staff_temp_password()}
				helper={m.staff_temp_password_helper()}
				bind:value={resetPassword}
				error={resetError}
				autocomplete="off"
				spellcheck="false"
				required
			/>
			<Button type="button" variant="secondary" onclick={() => resetDialog.close()}>{m.admin_cancel()}</Button>
			<Button type="submit" disabled={busy}>{m.staff_reset_password()}</Button>
		</form>
	{/if}
</dialog>

<style>
	.back {
		display: inline-block;
		margin-bottom: var(--space-md);
		font: var(--font-label);
		color: var(--color-ink);
	}
	header {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--space-sm);
		margin-bottom: var(--space-lg);
	}
	.muted {
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	.create {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
	}
	.fields {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr));
		align-items: start;
		gap: var(--space-md);
	}
	.add {
		margin-bottom: var(--space-lg);
	}
	.show-inactive {
		display: flex;
		align-items: center;
		gap: var(--space-xs);
		min-height: var(--size-control);
		margin-bottom: var(--space-sm);
		font: var(--font-body-sm);
		color: var(--color-ink);
		cursor: pointer;
	}
	.show-inactive input {
		width: var(--space-md);
		height: var(--space-md);
		margin: 0;
		accent-color: var(--color-primary);
	}
	/* Select and Save on one line; the select gives up width first. */
	.inline {
		display: flex;
		align-items: flex-end;
		gap: var(--space-xs);
	}
	.inline > :global(.field) {
		flex: 1 1 auto;
		min-width: 0;
	}
	.access {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-sm);
	}
	/* Status pill like {component.badge-pill}: tint, dot and text, so color is not the only signal. */
	.state {
		display: inline-flex;
		align-items: center;
		gap: var(--space-xs);
		padding: var(--space-xxs) var(--space-sm);
		border-radius: var(--radius-pill);
		background: var(--color-surface-card);
		font: var(--font-caption);
		color: var(--color-ink);
		white-space: nowrap;
	}
	.state .dot {
		width: var(--space-xs);
		height: var(--space-xs);
		border-radius: var(--radius-full);
		background: var(--color-muted-soft);
	}
	.state.active {
		background: var(--color-success-tint);
	}
	.state.active .dot {
		background: var(--color-success);
	}
	table {
		width: 100%;
		border-collapse: separate;
		border-spacing: 0;
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
		box-shadow: var(--shadow-sm);
		font: var(--font-body-sm);
		overflow: hidden;
	}
	th {
		padding: var(--space-sm) var(--space-md);
		background: var(--color-surface-soft);
		font: var(--font-label);
		color: var(--color-ink);
		text-align: start;
	}
	td {
		padding: var(--space-sm) var(--space-md);
		border-top: 1px solid var(--color-hairline);
		vertical-align: middle;
	}
	td.row-error {
		border-top: none;
		padding-top: 0;
	}
	.who {
		font: var(--font-title-sm);
		color: var(--color-ink);
	}
	.username {
		overflow-wrap: anywhere;
	}
	.nowrap {
		white-space: nowrap;
	}
	.cards {
		display: none;
		margin: 0;
		padding: 0;
		list-style: none;
		flex-direction: column;
		gap: var(--space-sm);
	}
	.cards li {
		display: flex;
		flex-direction: column;
		gap: var(--space-xs);
		padding: var(--space-md);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
		box-shadow: var(--shadow-sm);
	}
	.top {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-xs);
	}
	@media (max-width: 1023px) {
		table {
			display: none;
		}
		.cards {
			display: flex;
		}
	}
	dialog {
		max-width: min(92vw, 480px);
		padding: var(--space-lg);
		border: none;
		border-radius: var(--radius-xl);
		box-shadow: var(--shadow-md);
		color: var(--color-body);
	}
	dialog::backdrop {
		background: color-mix(in srgb, var(--color-surface-dark) 70%, transparent);
	}
	dialog h2 {
		margin: 0 0 var(--space-sm);
		font: var(--font-title-md);
		color: var(--color-ink);
	}
	dialog p {
		margin: 0 0 var(--space-lg);
	}
	dialog form {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-xs);
	}
	dialog form.reset :global(.field) {
		flex-basis: 100%;
	}
</style>
