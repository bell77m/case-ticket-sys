<!-- Activity log (T3.06, FR-L1, FR-I6) with its PDF export (FR-P4). Layout: DESIGN.md "Activity log". -->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { actionLabels } from '$lib/activity';
	import { ApiError, exportActivity } from '$lib/api';
	import ActivityTable from '$lib/components/ActivityTable.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import Select from '$lib/components/Select.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';
	import { saveFile } from '$lib/print';

	let { data } = $props();
	const params = $derived(page.url.searchParams);
	const log = $derived(data.allowed ? data.log : null);
	const fields = $derived(data.allowed ? data.fields : {});
	const pages = $derived(log ? Math.max(1, Math.ceil(log.total / log.page_size)) : 1);
	const num = new Intl.NumberFormat(getLocale());

	const actionOptions = [
		{ value: '', label: m.activity_all_actions() },
		...Object.entries(actionLabels).map(([value, label]) => ({ value, label: label() }))
	];
	// A staff member missing from the list (deactivated, for a viewer who reads active staff only) still shows as picked.
	const staffOptions = $derived.by(() => {
		const list = (data.allowed && data.staff) || [];
		const id = params.get('staff');
		const missing = id && !list.some((o) => o.value === id) ? [{ value: id, label: log?.items[0]?.actor.name ?? id }] : [];
		return [{ value: '', label: m.activity_all_staff() }, ...missing, ...list];
	});

	// Filters go to the URL, only through Apply (frontend.md: never on a select's change). A new filter starts at page 1.
	function apply(form: HTMLFormElement) {
		const next = new URLSearchParams();
		for (const [key, value] of new FormData(form)) {
			const v = String(value).trim().replace(/^#/, ''); // "#12" as the list shows ticket numbers
			if (v) next.set(key, v);
		}
		goto(`?${next}`, { keepFocus: true, noScroll: true, replaceState: true });
	}
	function toPage(n: number) {
		const next = new URLSearchParams(page.url.searchParams);
		next.set('page', String(n));
		goto(`?${next}`, { keepFocus: true });
	}

	// Export PDF (FR-P4): the applied filters, in the viewer's language (FR-I6). Gotenberg takes a few seconds.
	let exporting = $state(false);
	let exportError = $state('');
	async function exportPdf() {
		exporting = true;
		exportError = '';
		try {
			const { name, blob } = await exportActivity({
				staff: params.get('staff') ?? '',
				action: params.getAll('action'),
				ticket: params.get('ticket') ?? '',
				from: params.get('from') ?? '',
				to: params.get('to') ?? '',
				tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
				lang: getLocale()
			});
			saveFile(name, blob);
		} catch (err) {
			exportError = !(err instanceof ApiError)
				? m.err_network()
				: err.code === 'pdf.unavailable'
					? m.reports_err_pdf_unavailable()
					: err.code === 'validation'
						? m.detail_err_invalid()
						: err.status === 401
							? m.detail_err_session()
							: err.status === 403
								? m.detail_err_forbidden()
								: m.reports_err_pdf_failed();
		} finally {
			exporting = false;
		}
	}
</script>

<header>
	<h1>{m.activity_heading()}</h1>
	{#if log}
		<span class="muted">
			{log.total === 1 ? m.activity_total_one() : m.activity_total({ count: num.format(log.total) })}
		</span>
	{/if}
</header>

{#if !data.allowed}
	<Alert variant="error">{m.admin_forbidden()}</Alert>
{:else}
	<form
		class="filters"
		aria-label={m.activity_filters()}
		onsubmit={(e) => {
			e.preventDefault();
			apply(e.currentTarget);
		}}
	>
		{#if data.staff}
			<Select label={m.activity_filter_staff()} name="staff" options={staffOptions} value={params.get('staff') ?? ''} />
		{:else}
			<TextInput
				label={m.activity_filter_staff_id()}
				name="staff"
				inputmode="numeric"
				value={params.get('staff') ?? ''}
				error={fields.staff && m.detail_err_invalid()}
			/>
		{/if}
		<Select label={m.activity_filter_action()} name="action" options={actionOptions} value={params.get('action') ?? ''} />
		<TextInput
			label={m.activity_filter_ticket()}
			name="ticket"
			inputmode="numeric"
			value={params.get('ticket') ?? ''}
			error={fields.ticket && m.activity_err_ticket()}
		/>
		<TextInput
			label={m.reports_from()}
			type="date"
			name="from"
			value={params.get('from') ?? ''}
			error={fields.from && m.activity_err_dates()}
		/>
		<TextInput label={m.reports_to()} type="date" name="to" value={params.get('to') ?? ''} error={fields.to && m.activity_err_dates()} />
		<Button type="submit" variant="secondary">{m.reports_apply()}</Button>
		{#if log}
			<Button variant="secondary" disabled={exporting} onclick={exportPdf}>
				{exporting ? m.reports_exporting() : m.reports_export()}
			</Button>
		{/if}
	</form>

	{#if exportError}
		<Alert variant="error">{exportError}</Alert>
	{/if}
	{#if !log}
		<Alert variant="error">{m.detail_err_invalid()}</Alert>
	{:else if log.items.length}
		<ActivityTable items={log.items} />
		{#if pages > 1}
			<nav aria-label={m.queue_pages()}>
				<Button variant="secondary" disabled={log.page <= 1} onclick={() => toPage(log.page - 1)}>{m.queue_prev()}</Button>
				<span>{m.queue_page({ page: num.format(log.page), pages: num.format(pages) })}</span>
				<Button variant="secondary" disabled={log.page >= pages} onclick={() => toPage(log.page + 1)}>{m.queue_next()}</Button>
			</nav>
		{/if}
	{:else}
		<p class="empty">{m.activity_empty()}</p>
	{/if}
{/if}

<style>
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
	/* One row above the list; wraps two per row on phones (as the reports filter row). */
	.filters {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: var(--space-sm);
		margin-bottom: var(--space-lg);
	}
	.filters > :global(.field) {
		flex: 1 1 130px;
		min-width: 0;
	}
	.filters ~ :global(.alert) {
		margin-bottom: var(--space-lg);
	}
	nav {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-md);
		margin-top: var(--space-lg);
		font: var(--font-body-sm);
	}
	.empty {
		padding: var(--space-xl);
		border-radius: var(--radius-lg);
		background: var(--color-surface-soft);
		text-align: center;
		color: var(--color-muted);
	}
</style>
