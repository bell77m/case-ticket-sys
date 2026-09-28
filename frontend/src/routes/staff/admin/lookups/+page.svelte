<!-- Categories and locations (T2.11, FR-I4). Layout: DESIGN.md "Categories and locations". -->
<script lang="ts">
	import { tick } from 'svelte';
	import { invalidateAll } from '$app/navigation';
	import {
		ApiError,
		createCategory,
		createLocation,
		updateCategory,
		updateLocation,
		type AdminCategory,
		type AdminLocation,
		type Names
	} from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Card from '$lib/components/Card.svelte';
	import NamesFields from '$lib/components/NamesFields.svelte';
	import Select from '$lib/components/Select.svelte';
	import { showToast } from '$lib/components/toast-state.svelte';
	import { m } from '$lib/paraglide/messages';
	import { locales } from '$lib/paraglide/runtime';

	let { data } = $props();

	// API error codes, translated here (CLAUDE.md "i18n"). Field codes show under each name (NamesFields).
	const errorText: Record<string, () => string> = {
		'category.exists': m.lookups_err_category_exists,
		'location.exists': m.lookups_err_location_exists,
		'category.not_found': m.lookups_err_not_found,
		'location.not_found': m.lookups_err_not_found,
		'auth.forbidden': m.detail_err_forbidden,
		'auth.required': m.detail_err_session,
		validation: m.detail_err_invalid
	};
	const describeError = (err: unknown) =>
		err instanceof ApiError ? (errorText[err.code] ?? m.detail_err_failed)() : m.err_network();

	const blank = (): Names => ({ en: '', 'zh-CN': '', my: '', th: '' });
	const otherLanguages = locales.filter((l) => l !== 'en');

	let busy = $state(false);

	type Form = { errors: Record<string, string>; error: string };
	/** One write. Field codes go under their inputs; every failure also shows as an Alert in the form. */
	async function send(form: Form, call: () => Promise<unknown>, done: string) {
		form.errors = {};
		form.error = '';
		busy = true;
		try {
			await call();
			showToast(done);
			await invalidateAll();
			return true;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') form.errors = err.fields;
			form.error = describeError(err);
			return false;
		} finally {
			busy = false;
		}
	}

	// Locations grouped building → floor → line by English name, as the guest form groups them.
	type Group = { names: Names; floors: { names: Names; lines: AdminLocation[] }[] };
	const tree = $derived.by(() => {
		const out: Group[] = [];
		for (const l of data.locations) {
			let b = out.find((g) => g.names.en === l.building.en);
			if (!b) out.push((b = { names: l.building, floors: [] }));
			let f = b.floors.find((g) => g.names.en === l.floor.en);
			if (!f) b.floors.push((f = { names: l.floor, lines: [] }));
			f.lines.push(l);
		}
		return out;
	});

	// One category or line in the list: what it shows, and what Edit and Deactivate send.
	type Field = 'name' | 'building' | 'floor' | 'line';
	type Patch = Partial<Record<Field, Names>> & { is_active?: boolean };
	type Item = { key: string; label: string; names: Names; active: boolean; fields: () => [Field, Names][]; save: (p: Patch) => Promise<unknown> };
	const categoryItem = (c: AdminCategory): Item => ({
		key: `c${c.id}`,
		label: c.name.en,
		names: c.name,
		active: c.is_active,
		fields: () => [['name', { ...c.name }]],
		save: (p) => updateCategory(c.id, p)
	});
	const lineItem = (l: AdminLocation): Item => ({
		key: `l${l.id}`,
		label: `${l.building.en} / ${l.floor.en} / ${l.line.en}`,
		names: l.line,
		active: l.is_active,
		fields: () => [
			['building', { ...l.building }],
			['floor', { ...l.floor }],
			['line', { ...l.line }]
		],
		save: (p) => updateLocation(l.id, p)
	});
	const legends: Record<Field, () => string> = {
		name: m.lookups_category_name,
		building: m.form_building,
		floor: m.form_floor,
		line: m.form_line
	};

	// Edit: one row at a time, its names inline under the row.
	let edit = $state<(Form & { key: string; fields: [Field, Names][]; saved: string; save: Item['save'] }) | null>(null);
	function openEdit(it: Item) {
		const fields = it.fields();
		edit = { key: it.key, fields, saved: JSON.stringify(fields), save: it.save, errors: {}, error: '' };
	}
	// The form goes away, so focus goes back to its Edit button.
	async function closeEdit() {
		const key = edit?.key;
		edit = null;
		await tick();
		document.getElementById(`edit-${key}`)?.focus();
	}
	async function saveEdit(event: SubmitEvent) {
		event.preventDefault();
		const e = edit;
		if (!e) return;
		if (JSON.stringify(e.fields) === e.saved) {
			showToast(m.detail_no_changes());
			return closeEdit();
		}
		if (await send(e, () => e.save(Object.fromEntries(e.fields)), m.detail_saved())) closeEdit();
	}

	// Deactivate and reactivate ask first, in a native modal dialog (focus, Esc), as on the staff page.
	let dialog: HTMLDialogElement;
	let pending = $state.raw<Item | null>(null);
	let rowError = $state.raw<{ key: string; text: string } | null>(null);
	function ask(it: Item) {
		pending = it;
		dialog.showModal();
	}
	async function confirmActive() {
		dialog.close();
		const it = pending;
		if (!it) return;
		busy = true;
		rowError = null;
		try {
			await it.save({ is_active: !it.active });
			showToast(it.active ? m.lookups_deactivated({ name: it.label }) : m.lookups_reactivated({ name: it.label }));
		} catch (err) {
			rowError = { key: it.key, text: describeError(err) };
		}
		await invalidateAll();
		busy = false;
	}

	// New category.
	let newCategory = $state({ name: blank(), errors: {} as Record<string, string>, error: '' });
	async function addCategory(event: SubmitEvent) {
		event.preventDefault();
		if (await send(newCategory, () => createCategory(newCategory.name), m.lookups_category_added())) newCategory.name = blank();
	}

	// New line: an existing building and floor, or new ones. Selects only pick; nothing saves on change
	// (.claude/rules/frontend.md). Stored names are trimmed and never empty, so no building or floor is " ".
	const NEW = ' ';
	const freshLine = () => ({ b: '', f: '', building: blank(), floor: blank(), line: blank(), errors: {} as Record<string, string>, error: '' });
	let newLine = $state(freshLine());
	const building = $derived(tree.find((g) => g.names.en === newLine.b));
	const floor = $derived(building?.floors.find((g) => g.names.en === newLine.f));
	const newFloor = $derived(newLine.b === NEW || newLine.f === NEW);
	const choices = (groups: { names: Names }[], label: string) => [
		...groups.map((g) => ({ value: g.names.en, label: g.names.en })),
		{ value: NEW, label }
	];

	async function addLine(event: SubmitEvent) {
		event.preventDefault();
		const n = newLine;
		n.error = '';
		n.errors = !building && n.b !== NEW ? { building: 'required' } : !floor && !newFloor ? { floor: 'required' } : {};
		if (Object.keys(n.errors).length) return;
		// An existing building or floor: a copy of its names as loaded; the API groups lines by name.
		const line = {
			building: building ? { ...building.names } : n.building,
			floor: floor ? { ...floor.names } : n.floor,
			line: n.line
		};
		if (await send(n, () => createLocation(line), m.lookups_line_added())) newLine = freshLine();
	}
</script>

{#snippet translations(n: Names)}
	<span class="others">
		{#each otherLanguages as l (l)}<span lang={l}>{n[l]}</span>{/each}
	</span>
{/snippet}

{#snippet stateBadge(active: boolean)}
	<span class={['state', active && 'active']}>
		<span class="dot" aria-hidden="true"></span>{active ? m.staff_active() : m.staff_inactive()}
	</span>
{/snippet}

{#snippet row(it: Item)}
	<li class={['row', !it.active && 'inactive']}>
		<div class="top">
			<span class="name" lang="en">{it.names.en}</span>
			{@render stateBadge(it.active)}
		</div>
		{@render translations(it.names)}
		<div class="buttons">
			<Button
				variant="secondary"
				id="edit-{it.key}"
				aria-expanded={edit?.key === it.key}
				disabled={busy}
				onclick={() => (edit?.key === it.key ? closeEdit() : openEdit(it))}
			>
				{m.lookups_edit()}
			</Button>
			<Button variant="secondary" disabled={busy} onclick={() => ask(it)}>
				{it.active ? m.staff_deactivate() : m.staff_reactivate()}
			</Button>
		</div>
		{#if edit && edit.key === it.key}
			<form class="stack" onsubmit={saveEdit} novalidate>
				{#each edit.fields as field (field[0])}
					<NamesFields legend={legends[field[0]]()} prefix={field[0]} bind:value={field[1]} errors={edit.errors} />
				{/each}
				{#if edit.fields.length > 1}<p class="muted">{m.lookups_edit_hint()}</p>{/if}
				{#if edit.error}<Alert variant="error">{edit.error}</Alert>{/if}
				<div class="buttons">
					<Button type="submit" disabled={busy}>{m.admin_save()}</Button>
					<Button variant="secondary" onclick={closeEdit}>{m.admin_cancel()}</Button>
				</div>
			</form>
		{/if}
		{#if rowError?.key === it.key}<Alert variant="error">{rowError.text}</Alert>{/if}
	</li>
{/snippet}

<a class="back" href="/staff">{m.detail_back()}</a>

<header>
	<h1>{m.lookups_heading()}</h1>
</header>

{#if !data.categories}
	<Alert variant="error">{m.admin_forbidden()}</Alert>
{:else}
	<div class="sections">
		<Card title={m.lookups_categories()}>
			<form class="stack" onsubmit={addCategory} novalidate>
				<NamesFields legend={m.lookups_new_category()} prefix="name" bind:value={newCategory.name} errors={newCategory.errors} />
				{#if newCategory.error}<Alert variant="error">{newCategory.error}</Alert>{/if}
				<div><Button type="submit" disabled={busy}>{m.lookups_add_category()}</Button></div>
			</form>
			<ul class="rows">
				{#each data.categories as c (c.id)}{@render row(categoryItem(c))}{/each}
			</ul>
		</Card>

		<Card title={m.lookups_locations()}>
			<form class="stack" onsubmit={addLine} novalidate>
				<h3>{m.lookups_add_line_title()}</h3>
				<Select
					label={m.form_building()}
					placeholder={m.form_choose_building()}
					options={choices(tree, m.lookups_new_building())}
					bind:value={newLine.b}
					onchange={() => (newLine.f = '')}
					error={newLine.errors.building && m.err_required()}
				/>
				{#if newLine.b === NEW}
					<NamesFields legend={m.lookups_new_building()} prefix="building" bind:value={newLine.building} errors={newLine.errors} />
				{:else}
					<Select
						label={m.form_floor()}
						placeholder={newLine.b === '' ? m.form_choose_building_first() : m.form_choose_floor()}
						options={building ? choices(building.floors, m.lookups_new_floor()) : []}
						bind:value={newLine.f}
						disabled={newLine.b === ''}
						error={newLine.errors.floor && m.err_required()}
					/>
				{/if}
				{#if newFloor}
					<NamesFields legend={m.lookups_new_floor()} prefix="floor" bind:value={newLine.floor} errors={newLine.errors} />
				{/if}
				<NamesFields legend={m.lookups_new_line()} prefix="line" bind:value={newLine.line} errors={newLine.errors} />
				{#if newLine.error}<Alert variant="error">{newLine.error}</Alert>{/if}
				<div><Button type="submit" disabled={busy}>{m.lookups_add_line()}</Button></div>
			</form>
			<ul class="tree">
				{#each tree as b (b.names.en)}
					<li>
						<h3 lang="en">{b.names.en}</h3>
						{@render translations(b.names)}
						<ul class="floors">
							{#each b.floors as f (f.names.en)}
								<li>
									<h4 lang="en">{f.names.en}</h4>
									{@render translations(f.names)}
									<ul class="rows">
										{#each f.lines as l (l.id)}{@render row(lineItem(l))}{/each}
									</ul>
								</li>
							{/each}
						</ul>
					</li>
				{/each}
			</ul>
		</Card>
	</div>
{/if}

<dialog bind:this={dialog} aria-labelledby="confirm-title">
	{#if pending}
		<h2 id="confirm-title">
			{pending.active ? m.staff_deactivate_title({ name: pending.label }) : m.staff_reactivate_title({ name: pending.label })}
		</h2>
		<p>{pending.active ? m.lookups_deactivate_text() : m.lookups_reactivate_text()}</p>
		<form method="dialog">
			<Button type="submit" variant="secondary">{m.admin_cancel()}</Button>
			<Button variant={pending.active ? 'danger' : 'primary'} onclick={confirmActive}>
				{pending.active ? m.staff_deactivate() : m.staff_reactivate()}
			</Button>
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
		margin-bottom: var(--space-lg);
	}
	.muted {
		margin: 0;
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	/* Two columns from 1024px: categories stay short, locations grow. */
	.sections {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 480px), 1fr));
		align-items: start;
		gap: var(--space-lg);
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
	}
	h3 {
		margin: 0;
		font: var(--font-title-sm);
		color: var(--color-ink);
	}
	h4 {
		margin: 0;
		font: var(--font-label);
		color: var(--color-ink);
	}
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	/* The list sits under the add form, past a hairline. */
	.rows,
	.tree {
		display: flex;
		flex-direction: column;
	}
	.stack + .rows,
	.stack + .tree {
		margin-top: var(--space-lg);
		border-top: 1px solid var(--color-hairline);
	}
	.tree > li {
		padding: var(--space-md) 0;
		border-bottom: 1px solid var(--color-hairline);
	}
	.tree > li:last-child {
		border-bottom: none;
		padding-bottom: 0;
	}
	.floors > li {
		margin-top: var(--space-sm);
		padding-left: var(--space-md);
		border-left: 2px solid var(--color-hairline);
	}
	.row {
		display: flex;
		flex-direction: column;
		gap: var(--space-xs);
		padding: var(--space-sm) 0;
	}
	.rows > .row + .row {
		border-top: 1px solid var(--color-hairline);
	}
	.top {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-xs);
	}
	.name {
		font: var(--font-title-sm);
		color: var(--color-ink);
		overflow-wrap: anywhere;
	}
	/* Deactivated rows: muted text (still 4.5:1 on canvas), plus the badge, so color is not the only signal. */
	.inactive .name {
		color: var(--color-muted);
	}
	.others {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-xxs);
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	.others span + span::before {
		content: '· ' / '';
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-xs);
	}
	/* Status pill like {component.badge-pill}, as on the staff page. */
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
		overflow-wrap: anywhere;
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
</style>
