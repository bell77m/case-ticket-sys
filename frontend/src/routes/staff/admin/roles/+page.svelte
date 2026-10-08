<!-- Roles as a permission grid (T2.10, FR-R2, FR-A3). Layout: DESIGN.md "Roles". -->
<script lang="ts">
	import { beforeNavigate, invalidateAll } from '$app/navigation';
	import { ApiError, createRole, setRolePermissions, type Role } from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import { showToast } from '$lib/components/toast-state.svelte';
	import { permissions } from '$lib/permissions';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';

	let { data } = $props();

	// Display only: the API checks role.manage again (FR-R3). Everyone else with staff.manage reads the grid.
	const canEdit = $derived(data.me.permissions.includes('role.manage'));

	// FR-A3: kept for the Root Admin role; the API refuses them on any other role.
	const rootOnly = (p: string) => p === 'staff.create' || p === 'role.manage';
	// The Root Admin role is fixed (the API answers role.root_fixed).
	const editable = (r: Role) => canEdit && !r.root;

	const errorText: Record<string, () => string> = {
		'role.root_fixed': m.roles_err_root_fixed,
		'role.not_found': m.roles_err_not_found,
		'auth.forbidden': m.detail_err_forbidden,
		'auth.required': m.detail_err_session,
		validation: m.detail_err_invalid
	};
	// Field codes for name and permissions (validation).
	const fieldText: Record<string, () => string> = {
		required: m.err_required,
		too_long: m.err_too_long,
		invalid_characters: m.err_invalid_characters,
		taken: m.roles_err_name_taken,
		invalid: m.roles_err_permission_invalid,
		root_only: m.roles_err_root_only
	};
	function describeError(err: unknown) {
		if (!(err instanceof ApiError)) return m.err_network();
		if (err.fields.permissions) return (fieldText[err.fields.permissions] ?? m.detail_err_invalid)();
		return (errorText[err.code] ?? m.detail_err_failed)();
	}

	let busy = $state(false);

	// Unsaved checkbox changes per role; the saved set while untouched.
	let drafts = $state<Record<number, string[]>>({});
	const granted = (r: Role) => drafts[r.id] ?? r.permissions;
	function toggle(r: Role, p: string, on: boolean) {
		const rest = granted(r).filter((x) => x !== p);
		drafts[r.id] = on ? [...rest, p] : rest;
	}
	// A cell differs from the saved role; a role with any such cell has unsaved changes.
	const changed = (r: Role, p: string) => !!drafts[r.id] && drafts[r.id].includes(p) !== r.permissions.includes(p);
	const dirty = (r: Role) => permissions.some(([p]) => changed(r, p));
	function discard(r: Role) {
		delete drafts[r.id];
		if (saveError?.id === r.id) saveError = null;
	}
	// Leaving with unsaved ticks asks first; closing the tab gets the browser's own prompt.
	beforeNavigate(({ cancel, type }) => {
		if (!data.roles?.some(dirty)) return;
		if (type === 'leave' || !confirm(m.roles_leave_confirm())) cancel();
	});

	let saveError = $state.raw<{ id: number; text: string } | null>(null);

	async function save(r: Role) {
		if (!dirty(r)) return showToast(m.detail_no_changes());
		busy = true;
		saveError = null;
		try {
			await setRolePermissions(r.id, drafts[r.id]);
			delete drafts[r.id];
			showToast(m.detail_saved());
		} catch (err) {
			saveError = { id: r.id, text: describeError(err) };
		}
		await invalidateAll();
		busy = false;
	}

	// New role (role.manage).
	let name = $state('');
	let picked = $state<string[]>([]);
	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	const fieldError = (f: string) => (errors[f] ? (fieldText[errors[f]] ?? m.err_required)() : undefined);

	async function create(event: SubmitEvent) {
		event.preventDefault();
		formError = '';
		errors = name.trim() ? {} : { name: 'required' };
		if (errors.name) return;
		busy = true;
		try {
			await createRole(name, picked);
			name = '';
			picked = [];
			showToast(m.roles_created());
			await invalidateAll();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') errors = err.fields;
			else if (err instanceof ApiError && err.code === 'role.name_taken') errors = { name: 'taken' };
			else formError = describeError(err);
		} finally {
			busy = false;
		}
	}

	const numberFmt = new Intl.NumberFormat(getLocale());
	const staffCount = (r: Role) =>
		r.staff_count === 1 ? m.roles_staff_one() : m.roles_staff({ count: numberFmt.format(r.staff_count) });
</script>

<!-- A read-only cell: a check or a dash, with the words for screen readers. -->
{#snippet mark(on: boolean)}
	{#if on}
		<svg class="check" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 6 9 17l-5-5" /></svg>
	{:else}
		<span class="none" aria-hidden="true">–</span>
	{/if}
	<span class="visually-hidden">{on ? m.roles_granted() : m.roles_not_granted()}</span>
{/snippet}

{#snippet checkbox(r: Role, p: string, label?: string)}
	<input
		type="checkbox"
		aria-label={label}
		checked={granted(r).includes(p)}
		disabled={rootOnly(p)}
		onchange={(e) => toggle(r, p, e.currentTarget.checked)}
	/>
{/snippet}

{#snippet permLabel(p: string, label: () => string)}
	<span class="perm-name">
		{label()}
		{#if rootOnly(p)}<span class="hint">{m.roles_root_only()}</span>{/if}
	</span>
{/snippet}

{#snippet unsaved(r: Role)}
	{#if dirty(r)}<span class="unsaved"><span class="dot" aria-hidden="true"></span>{m.roles_unsaved()}</span>{/if}
{/snippet}

{#snippet footer(r: Role)}
	{#if editable(r)}
		<Button variant={dirty(r) ? 'primary' : 'secondary'} disabled={busy} onclick={() => save(r)}>{m.roles_save({ role: r.name })}</Button>
		{#if dirty(r)}
			<Button variant="secondary" disabled={busy} onclick={() => discard(r)}>{m.roles_discard({ role: r.name })}</Button>
		{/if}
	{:else if canEdit}
		<span class="muted">{m.roles_fixed()}</span>
	{/if}
	{#if saveError?.id === r.id}<Alert variant="error">{saveError.text}</Alert>{/if}
{/snippet}

<header>
	<h1>{m.roles_heading()}</h1>
</header>

{#if !data.roles}
	<Alert variant="error">{m.admin_forbidden()}</Alert>
{:else}
	{#if !canEdit}<p class="muted intro">{m.roles_readonly()}</p>{/if}

	<!-- From 1024px: permissions as rows, roles as columns. Narrower: one card per role. -->
	<table>
		<caption class="visually-hidden">{m.roles_caption()}</caption>
		<thead>
			<tr>
				<th scope="col">{m.roles_permission()}</th>
				{#each data.roles as r (r.id)}
					<th scope="col">
						<span class="role-name">{r.name}</span>
						<span class="count">{staffCount(r)}</span>
						{@render unsaved(r)}
					</th>
				{/each}
			</tr>
		</thead>
		<tbody>
			{#each permissions as [p, label] (p)}
				<tr>
					<th scope="row">{@render permLabel(p, label)}</th>
					{#each data.roles as r (r.id)}
						<td class={['cell', changed(r, p) && 'changed']}>
							{#if editable(r)}
								<label class="tick">{@render checkbox(r, p, m.roles_grant({ role: r.name, permission: label() }))}</label>
							{:else}
								{@render mark(r.permissions.includes(p))}
							{/if}
						</td>
					{/each}
				</tr>
			{/each}
		</tbody>
		{#if canEdit}
			<tfoot>
				<tr>
					<td></td>
					{#each data.roles as r (r.id)}
						<td class="cell"><div class="foot">{@render footer(r)}</div></td>
					{/each}
				</tr>
			</tfoot>
		{/if}
	</table>

	<ul class="cards">
		{#each data.roles as r (r.id)}
			<li>
				<div class="top">
					<h2>{r.name}</h2>
					<span class="muted">{staffCount(r)}</span>
					{@render unsaved(r)}
				</div>
				<ul class="perms">
					{#each permissions as [p, label] (p)}
						<li class={[changed(r, p) && 'changed']}>
							{#if editable(r)}
								<label class="row">{@render checkbox(r, p)} {@render permLabel(p, label)}</label>
							{:else}
								<span class="row">{@render mark(r.permissions.includes(p))} {@render permLabel(p, label)}</span>
							{/if}
						</li>
					{/each}
				</ul>
				{@render footer(r)}
			</li>
		{/each}
	</ul>

	{#if canEdit}
		<div class="new">
			<Card title={m.roles_new_title()}>
				<form onsubmit={create} novalidate>
					<TextInput label={m.roles_name()} bind:value={name} error={fieldError('name')} maxlength={50} autocomplete="off" required />
					<fieldset aria-describedby={errors.permissions ? 'perm-error' : undefined}>
						<legend>{m.roles_permissions()}</legend>
						{#each permissions as [p, label] (p)}
							<label class="row">
								<input type="checkbox" value={p} bind:group={picked} disabled={rootOnly(p)} />
								{@render permLabel(p, label)}
							</label>
						{/each}
						{#if errors.permissions}<p id="perm-error" class="error">{fieldError('permissions')}</p>{/if}
					</fieldset>
					{#if formError}<Alert variant="error">{formError}</Alert>{/if}
					<div><Button type="submit" disabled={busy}>{m.roles_create()}</Button></div>
				</form>
			</Card>
		</div>
	{/if}
{/if}

<style>
	header {
		margin-bottom: var(--space-lg);
	}
	.muted {
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	.intro {
		margin: 0 0 var(--space-lg);
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
	th,
	td {
		padding: var(--space-xs) var(--space-md);
		border-top: 1px solid var(--color-hairline);
		text-align: start;
		vertical-align: middle;
	}
	thead th {
		border-top: none;
		background: var(--color-surface-soft);
		vertical-align: bottom;
	}
	th {
		font: var(--font-label);
		color: var(--color-ink);
	}
	tbody th {
		font: var(--font-body-sm);
		color: var(--color-ink);
	}
	.role-name,
	.count {
		display: block;
	}
	.count {
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	.cell {
		text-align: center;
	}
	/* A tick that differs from the saved role, on warning tint; the "Unsaved changes" pill says it in words. */
	.changed {
		background: var(--color-warning-tint);
	}
	.perms .changed {
		margin-inline: calc(-1 * var(--space-xs));
		padding-inline: var(--space-xs);
		border-radius: var(--radius-md);
	}
	/* Badge-pill like the status badges: tint, dot and ink text. */
	.unsaved {
		display: inline-flex;
		align-items: center;
		gap: var(--space-xs);
		margin-top: var(--space-xxs);
		padding: var(--space-xxs) var(--space-sm);
		border-radius: var(--radius-pill);
		background: var(--color-warning-tint);
		font: var(--font-caption);
		color: var(--color-ink);
		white-space: nowrap;
	}
	.unsaved .dot {
		width: var(--space-xs);
		height: var(--space-xs);
		border-radius: var(--radius-full);
		background: var(--color-warning);
	}
	tfoot .cell {
		vertical-align: top;
	}
	.foot {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-xs);
	}
	.perm-name {
		display: flex;
		flex-direction: column;
	}
	.hint {
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	.check {
		width: var(--space-lg);
		height: var(--space-lg);
		fill: none;
		stroke: var(--color-ink); /* 1.4.11: success green is under 3:1 on canvas */
		stroke-width: 2.5;
		stroke-linecap: round;
		stroke-linejoin: round;
		vertical-align: middle;
	}
	.none {
		display: inline-block;
		width: var(--space-lg);
		text-align: center;
		color: var(--color-muted);
	}
	/* 44px tap target around each checkbox. */
	.tick {
		display: inline-grid;
		place-items: center;
		min-width: var(--size-control);
		min-height: var(--size-control);
		cursor: pointer;
	}
	input[type='checkbox'] {
		flex: none;
		width: var(--space-md);
		height: var(--space-md);
		margin: 0;
		accent-color: var(--color-primary);
		cursor: pointer;
	}
	input[type='checkbox']:disabled {
		cursor: not-allowed;
	}
	.cards {
		display: none;
		margin: 0;
		padding: 0;
		list-style: none;
		flex-direction: column;
		gap: var(--space-sm);
	}
	.cards > li {
		display: flex;
		flex-direction: column;
		gap: var(--space-sm);
		padding: var(--space-md);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
		box-shadow: var(--shadow-sm);
	}
	.top {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		justify-content: space-between;
		gap: var(--space-xs);
	}
	h2 {
		margin: 0;
		font: var(--font-title-md);
		color: var(--color-ink);
	}
	.perms {
		margin: 0;
		padding: 0;
		list-style: none;
		font: var(--font-body-sm);
		color: var(--color-ink);
	}
	.row {
		display: flex;
		align-items: center;
		gap: var(--space-sm);
		min-height: var(--size-control);
	}
	label.row {
		cursor: pointer;
	}
	@media (max-width: 1023px) {
		table {
			display: none;
		}
		.cards {
			display: flex;
		}
	}
	.new {
		margin-top: var(--space-lg);
	}
	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
		max-width: 640px;
	}
	fieldset {
		margin: 0;
		padding: 0;
		border: none;
		font: var(--font-body-sm);
		color: var(--color-ink);
	}
	legend {
		padding: 0;
		margin-bottom: var(--space-xxs);
		font: var(--font-label);
		color: var(--color-ink);
	}
	.error {
		margin: var(--space-xxs) 0 0;
		font: var(--font-body-sm);
		color: var(--color-error-strong);
	}
</style>
