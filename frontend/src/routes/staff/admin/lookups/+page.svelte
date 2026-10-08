<!-- Categories and locations (T2.11, FR-I4). Layout: DESIGN.md "Categories and locations". -->
<script lang="ts">
	import { tick } from 'svelte';
	import { invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
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
	import { languageNames } from '$lib/LanguageSwitcher.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import NamesFields from '$lib/components/NamesFields.svelte';
	import Select from '$lib/components/Select.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import { showToast } from '$lib/components/toast-state.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale, locales } from '$lib/paraglide/runtime';

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
	const num = new Intl.NumberFormat(getLocale());

	// One list at a time; the tab lives in the URL, so a reload or a shared link opens the same one.
	const tab = $derived(page.url.searchParams.get('tab') === 'locations' ? 'locations' : 'categories');

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
	function group(locations: AdminLocation[]) {
		const out: Group[] = [];
		for (const l of locations) {
			let b = out.find((g) => g.names.en === l.building.en);
			if (!b) out.push((b = { names: l.building, floors: [] }));
			let f = b.floors.find((g) => g.names.en === l.floor.en);
			if (!f) b.floors.push((f = { names: l.floor, lines: [] }));
			f.lines.push(l);
		}
		return out;
	}
	// Deactivated rows pile up (there is no delete), so they stay out of the lists and the add form's selects until
	// asked for. A row changed on this visit stays, so focus does not drop when its button goes away.
	let showInactive = $state(false);
	let kept = $state<string>();
	const visible = (active: boolean, key: string) => active || showInactive || key === kept;
	const inactive = $derived(
		(data.categories?.filter((c) => !c.is_active).length ?? 0) + data.locations.filter((l) => !l.is_active).length
	);
	const tree = $derived(group(data.locations.filter((l) => l.is_active || showInactive)));

	// Search narrows the open list as you type, on any of the four names (display only).
	let query = $state('');
	const q = $derived(query.trim().toLocaleLowerCase());
	const hit = (...names: Names[]) => !q || names.some((n) => Object.values(n).some((s) => s.toLocaleLowerCase().includes(q)));
	const shownCategories = $derived(data.categories?.filter((c) => visible(c.is_active, `c${c.id}`) && hit(c.name)) ?? []);
	const shownLines = $derived(data.locations.filter((l) => visible(l.is_active, `l${l.id}`) && hit(l.building, l.floor, l.line)));
	const shownTree = $derived(group(shownLines));
	const shownCount = $derived(tab === 'categories' ? shownCategories.length : shownLines.length);

	// One category or line in the list: what it shows, and what Edit and Deactivate send.
	type Field = 'name' | 'building' | 'floor' | 'line';
	type Patch = Partial<Record<Field, Names>> & { is_active?: boolean };
	type Item = { key: string; label: string; active: boolean; fields: () => [Field, Names][]; save: (p: Patch) => Promise<unknown> };
	const categoryItem = (c: AdminCategory): Item => ({
		key: `c${c.id}`,
		label: c.name.en,
		active: c.is_active,
		fields: () => [['name', { ...c.name }]],
		save: (p) => updateCategory(c.id, p)
	});
	const lineItem = (l: AdminLocation): Item => ({
		key: `l${l.id}`,
		label: `${l.building.en} / ${l.floor.en} / ${l.line.en}`,
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

	// Edit: the names in a modal dialog.
	let editDialog: HTMLDialogElement;
	let edit = $state<(Form & { key: string; label: string; fields: [Field, Names][]; saved: string; save: Item['save'] }) | null>(
		null
	);
	function openEdit(it: Item) {
		const fields = it.fields();
		edit = { key: it.key, label: it.label, fields, saved: JSON.stringify(fields), save: it.save, errors: {}, error: '' };
		editDialog.showModal();
	}
	// Table and cards both hold the row, so focus goes back to the Edit button that is on screen.
	async function closeEdit() {
		const key = edit?.key;
		editDialog.close();
		edit = null;
		await tick();
		[...document.querySelectorAll<HTMLElement>(`[data-edit="${key}"]`)].find((b) => b.offsetParent)?.focus();
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
		kept = it.key;
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

	// Add: the open tab's form in a modal dialog. What was typed stays when it is closed without saving.
	let addDialog: HTMLDialogElement;

	let newCategory = $state({ name: blank(), errors: {} as Record<string, string>, error: '' });
	async function addCategory(event: SubmitEvent) {
		event.preventDefault();
		if (await send(newCategory, () => createCategory(newCategory.name), m.lookups_category_added())) {
			newCategory.name = blank();
			addDialog.close();
		}
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
		if (await send(n, () => createLocation(line), m.lookups_line_added())) {
			newLine = freshLine();
			addDialog.close();
		}
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

<!-- Status and the row's buttons: the last table cell, or the foot of a card. -->
{#snippet actions(it: Item)}
	<div class="actions">
		{@render stateBadge(it.active)}
		<div class="buttons">
			<Button variant="secondary" data-edit={it.key} disabled={busy} onclick={() => openEdit(it)}>{m.lookups_edit()}</Button>
			<Button variant="secondary" disabled={busy} onclick={() => ask(it)}>
				{it.active ? m.staff_deactivate() : m.staff_reactivate()}
			</Button>
		</div>
	</div>
	{#if rowError?.key === it.key}<Alert variant="error">{rowError.text}</Alert>{/if}
{/snippet}

{#snippet noMatch(empty: boolean)}
	<p class="no-match" role="status">{q && empty ? m.admin_no_match() : ''}</p>
{/snippet}

<header>
	<h1>{m.lookups_heading()}</h1>
</header>

{#if !data.categories}
	<Alert variant="error">{m.admin_forbidden()}</Alert>
{:else}
	<div class="toolbar">
		<nav class="tabs" aria-label={m.lookups_heading()}>
			<a href="?tab=categories" data-sveltekit-replacestate data-sveltekit-noscroll aria-current={tab === 'categories' ? 'page' : undefined}>
				{m.lookups_categories()}<span class="count">{num.format(data.categories.filter((c) => c.is_active).length)}</span>
			</a>
			<a href="?tab=locations" data-sveltekit-replacestate data-sveltekit-noscroll aria-current={tab === 'locations' ? 'page' : undefined}>
				{m.lookups_locations()}<span class="count">{num.format(data.locations.filter((l) => l.is_active).length)}</span>
			</a>
		</nav>
		<Button onclick={() => addDialog.showModal()}>
			<svg class="plus" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
			{tab === 'categories' ? m.lookups_new_category() : m.lookups_add_line_title()}
		</Button>
	</div>

	<div class="list-tools">
		<TextInput
			label={m.admin_filter()}
			helper={m.lookups_filter_helper()}
			type="search"
			bind:value={query}
			maxlength={100}
			autocomplete="off"
			spellcheck="false"
		/>
		{#if inactive}
			<label class="show-inactive">
				<input type="checkbox" bind:checked={showInactive} />
				{m.lookups_show_inactive({ count: num.format(inactive) })}
			</label>
		{/if}
	</div>

	<!-- From 1024px: a table. Narrower: the same rows as stacked cards (DESIGN.md "Staff queue"). -->
	{#if tab === 'categories'}
		{#if shownCategories.length}
			<table>
				<caption class="visually-hidden">{m.lookups_categories()}</caption>
				<thead>
					<tr>
						{#each locales as l (l)}<th scope="col" lang={l}>{languageNames[l]}</th>{/each}
						<th scope="col">{m.queue_col_status()}</th>
					</tr>
				</thead>
				<tbody>
					{#each shownCategories as c (c.id)}
						<tr class={[!c.is_active && 'inactive']}>
							{#each locales as l (l)}<td lang={l} class={[l === 'en' && 'name']}>{c.name[l]}</td>{/each}
							<td>{@render actions(categoryItem(c))}</td>
						</tr>
					{/each}
				</tbody>
			</table>
			<ul class="cards">
				{#each shownCategories as c (c.id)}
					<li class={[!c.is_active && 'inactive']}>
						<span class="name" lang="en">{c.name.en}</span>
						{@render translations(c.name)}
						{@render actions(categoryItem(c))}
					</li>
				{/each}
			</ul>
		{/if}
		{@render noMatch(!shownCategories.length)}
	{:else}
		{#if shownLines.length}
			<table>
				<caption class="visually-hidden">{m.lookups_locations()}</caption>
				<thead>
					<tr>
						<th scope="col" rowspan="2">{m.form_floor()}</th>
						<th scope="colgroup" colspan={locales.length}>{m.form_line()}</th>
						<th scope="col" rowspan="2">{m.queue_col_status()}</th>
					</tr>
					<tr>
						{#each locales as l (l)}<th scope="col" lang={l}>{languageNames[l]}</th>{/each}
					</tr>
				</thead>
				{#each shownTree as b (b.names.en)}
					<tbody>
						<tr class="building">
							<th scope="rowgroup" colspan={locales.length + 2}>
								<span class="name" lang="en">{b.names.en}</span>
								{@render translations(b.names)}
							</th>
						</tr>
						{#each b.floors as f (f.names.en)}
							{#each f.lines as l, i (l.id)}
								<tr class={[!l.is_active && 'inactive']}>
									{#if i === 0}
										<td rowspan={f.lines.length} class="floor">
											<span lang="en">{f.names.en}</span>
											{@render translations(f.names)}
										</td>
									{/if}
									{#each locales as lang (lang)}<td {lang} class={[lang === 'en' && 'name']}>{l.line[lang]}</td>{/each}
									<td>{@render actions(lineItem(l))}</td>
								</tr>
							{/each}
						{/each}
					</tbody>
				{/each}
			</table>
			<div class="card-groups">
				{#each shownTree as b (b.names.en)}
					<section>
						<h2><span lang="en">{b.names.en}</span></h2>
						{@render translations(b.names)}
						<ul class="cards">
							{#each b.floors as f (f.names.en)}
								{#each f.lines as l (l.id)}
									<li class={[!l.is_active && 'inactive']}>
										<span class="floor-label" lang="en">{f.names.en}</span>
										<span class="name" lang="en">{l.line.en}</span>
										{@render translations(l.line)}
										{@render actions(lineItem(l))}
									</li>
								{/each}
							{/each}
						</ul>
					</section>
				{/each}
			</div>
		{/if}
		{@render noMatch(!shownLines.length)}
	{/if}
{/if}

<dialog bind:this={addDialog} aria-labelledby="add-title">
	<h2 id="add-title">{tab === 'categories' ? m.lookups_new_category() : m.lookups_add_line_title()}</h2>
	{#if tab === 'categories'}
		<form class="stack" onsubmit={addCategory} novalidate>
			<NamesFields legend={m.lookups_category_name()} prefix="name" bind:value={newCategory.name} errors={newCategory.errors} />
			{#if newCategory.error}<Alert variant="error">{newCategory.error}</Alert>{/if}
			<div class="dialog-buttons">
				<Button variant="secondary" onclick={() => addDialog.close()}>{m.admin_cancel()}</Button>
				<Button type="submit" disabled={busy}>{m.lookups_add_category()}</Button>
			</div>
		</form>
	{:else}
		<form class="stack" onsubmit={addLine} novalidate>
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
			<div class="dialog-buttons">
				<Button variant="secondary" onclick={() => addDialog.close()}>{m.admin_cancel()}</Button>
				<Button type="submit" disabled={busy}>{m.lookups_add_line()}</Button>
			</div>
		</form>
	{/if}
</dialog>

<dialog bind:this={editDialog} aria-labelledby="edit-title" onclose={() => (edit = null)}>
	{#if edit}
		<h2 id="edit-title">{m.lookups_edit_title({ name: edit.label })}</h2>
		<form class="stack" onsubmit={saveEdit} novalidate>
			{#each edit.fields as field (field[0])}
				<NamesFields legend={legends[field[0]]()} prefix={field[0]} bind:value={field[1]} errors={edit.errors} />
			{/each}
			{#if edit.fields.length > 1}<p class="muted">{m.lookups_edit_hint()}</p>{/if}
			{#if edit.error}<Alert variant="error">{edit.error}</Alert>{/if}
			<div class="dialog-buttons">
				<Button variant="secondary" onclick={closeEdit}>{m.admin_cancel()}</Button>
				<Button type="submit" disabled={busy}>{m.admin_save()}</Button>
			</div>
		</form>
	{/if}
</dialog>

<dialog bind:this={dialog} aria-labelledby="confirm-title">
	{#if pending}
		<h2 id="confirm-title">
			{pending.active ? m.staff_deactivate_title({ name: pending.label }) : m.staff_reactivate_title({ name: pending.label })}
		</h2>
		<p>{pending.active ? m.lookups_deactivate_text() : m.lookups_reactivate_text()}</p>
		<form method="dialog" class="dialog-buttons">
			<Button type="submit" variant="secondary">{m.admin_cancel()}</Button>
			<Button variant={pending.active ? 'danger' : 'primary'} onclick={confirmActive}>
				{pending.active ? m.staff_deactivate() : m.staff_reactivate()}
			</Button>
		</form>
	{/if}
</dialog>

<style>
	header {
		margin-bottom: var(--space-lg);
	}
	.muted {
		margin: 0;
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	/* Tabs on the left, the add button on the right; the button drops under the tabs on phones. */
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-sm);
		margin-bottom: var(--space-md);
	}
	/* nav-pill-group, as the admin tabs. */
	.tabs {
		display: flex;
		gap: var(--space-xxs);
		padding: var(--space-xxs);
		border-radius: var(--radius-lg);
		background: var(--color-surface-soft);
	}
	.tabs a {
		display: inline-flex;
		align-items: center;
		gap: var(--space-xs);
		min-height: var(--size-control);
		padding: var(--space-xs) var(--space-md);
		border-radius: var(--radius-md);
		font: var(--font-nav-link);
		color: var(--color-muted);
		text-decoration: none;
		transition: background-color var(--motion-fast);
	}
	.tabs a[aria-current='page'] {
		background: var(--color-canvas);
		color: var(--color-ink);
		box-shadow: var(--shadow-sm);
	}
	.count {
		padding: 0 var(--space-xs);
		border-radius: var(--radius-pill);
		background: var(--color-canvas);
		font: var(--font-caption);
		color: var(--color-ink);
	}
	.tabs a[aria-current='page'] .count {
		background: var(--color-surface-card);
	}
	.plus {
		width: var(--space-md);
		height: var(--space-md);
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
	}
	/* Search and the deactivated checkbox share a row, as on the staff page; they wrap on phones. */
	.list-tools {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-sm) var(--space-lg);
		margin-bottom: var(--space-md);
	}
	.list-tools > :global(.field) {
		flex: 0 1 320px;
	}
	.show-inactive {
		display: flex;
		align-items: center;
		gap: var(--space-xs);
		min-height: var(--size-control);
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
	.no-match {
		margin: 0;
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	/* Table like the staff accounts table: hairline card, header row on surface-soft. */
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
	thead tr + tr th,
	thead th[colspan] {
		border-bottom: none;
	}
	thead tr + tr th {
		padding-top: 0;
	}
	td {
		padding: var(--space-sm) var(--space-md);
		border-top: 1px solid var(--color-hairline);
		vertical-align: middle;
		color: var(--color-body);
		overflow-wrap: anywhere;
	}
	/* Building rows head their lines: title type on canvas, under a hairline. */
	tr.building th {
		border-top: 1px solid var(--color-hairline);
		background: var(--color-canvas);
		font: var(--font-body-sm);
	}
	td.floor {
		vertical-align: top;
		border-right: 1px solid var(--color-hairline);
		color: var(--color-ink);
	}
	.name {
		font: var(--font-title-sm);
		color: var(--color-ink);
		overflow-wrap: anywhere;
	}
	td.name {
		font: var(--font-label);
	}
	/* Deactivated rows: muted names (still 4.5:1 on canvas), plus the badge, so color is not the only signal. */
	.inactive .name,
	.inactive td[lang] {
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
	.actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-sm);
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
	/* Cards below 1024px. */
	.cards,
	.card-groups {
		display: none;
		flex-direction: column;
		gap: var(--space-sm);
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.card-groups {
		gap: var(--space-lg);
	}
	.card-groups section > .others {
		margin-bottom: var(--space-sm);
	}
	.card-groups h2 {
		margin: 0;
		font: var(--font-title-md);
		color: var(--color-ink);
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
	.floor-label {
		font: var(--font-caption);
		color: var(--color-muted);
	}
	@media (max-width: 1023px) {
		table {
			display: none;
		}
		.cards,
		.card-groups {
			display: flex;
		}
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
	}
	dialog {
		width: min(92vw, 640px);
		padding: var(--space-lg);
		border: none;
		border-radius: var(--radius-xl);
		box-shadow: var(--shadow-md);
		color: var(--color-body);
	}
	dialog[aria-labelledby='confirm-title'] {
		width: min(92vw, 480px);
	}
	dialog::backdrop {
		background: color-mix(in srgb, var(--color-surface-dark) 70%, transparent);
	}
	dialog h2 {
		margin: 0 0 var(--space-md);
		font: var(--font-title-md);
		color: var(--color-ink);
		overflow-wrap: anywhere;
	}
	dialog p {
		margin: 0 0 var(--space-lg);
	}
	.dialog-buttons {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-xs);
	}
</style>
