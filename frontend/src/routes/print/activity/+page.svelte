<!-- Activity log print page (T3.06, FR-P4, FR-I6): what Gotenberg prints to PDF. Layout: DESIGN.md "Activity print page". -->
<!-- No staff chrome (routes/+layout.svelte leaves it out on /print/) and no session: a one-time token in the URL fragment. -->
<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { actionLabel } from '$lib/activity';
	import { getPrintActivity, type PrintActivity } from '$lib/api';
	import ActivityTable from '$lib/components/ActivityTable.svelte';
	import Alert from '$lib/components/Alert.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';
	import { failPrint, fontsLoaded, loadPrint } from '$lib/print';

	let log = $state<PrintActivity>();
	let error = $state('');

	// Read once the log (and so the language) is in. Filter dates are calendar days, formatted as such.
	const period = $derived.by(() => {
		const f = log?.filters;
		if (!f) return '';
		const fmt = new Intl.DateTimeFormat(getLocale(), { dateStyle: 'medium', timeZone: 'UTC', calendar: 'gregory' });
		const day = (d: string) => new Date(d);
		if (f.from && f.to) return fmt.formatRange(day(f.from), day(f.to));
		if (f.from) return m.activity_period_from({ date: fmt.format(day(f.from)) });
		if (f.to) return m.activity_period_to({ date: fmt.format(day(f.to)) });
		return m.activity_period_all();
	});

	/** Shows the message, then fails the print (see failPrint). */
	function fail(message: string) {
		if (error) return;
		error = message;
		failPrint();
	}

	onMount(async () => {
		const data = await loadPrint(getPrintActivity);
		if (typeof data === 'string') return fail(data === 'expired' ? m.activity_print_expired() : m.activity_print_failed());
		try {
			log = data;
			await tick();
			await fontsLoaded();
			if (!error) window.printReady = true; // Gotenberg prints now
		} catch {
			fail(m.activity_print_failed());
		}
	});
</script>

<svelte:head>
	<title>{log ? m.activity_heading() : m.app_title()}</title>
</svelte:head>

<div class="sheet">
	{#if error}
		<Alert variant="error">{error}</Alert>
	{:else if log}
		<svelte:boundary onerror={() => fail(m.activity_print_failed())}>
			<header>
				<h1>{m.activity_heading()}</h1>
				<p>{m.print_period({ period })}</p>
				<p>{m.activity_print_staff({ staff: log.filters.staff?.name ?? m.activity_all_staff() })}</p>
				<p>
					{m.activity_print_action({
						action: log.filters.action.length ? log.filters.action.map(actionLabel).join(', ') : m.activity_all_actions()
					})}
				</p>
				<p>
					{m.activity_print_ticket({
						ticket: log.filters.ticket ? m.queue_ticket_no({ id: String(log.filters.ticket) }) : m.activity_all_tickets()
					})}
				</p>
				<p>{m.activity_print_tz({ tz: log.filters.tz })}</p>
				{#if log.truncated}
					<p><strong>{m.activity_print_truncated({ count: new Intl.NumberFormat(getLocale()).format(log.items.length) })}</strong></p>
				{/if}
			</header>
			{#if log.items.length}
				<ActivityTable items={log.items} print timeZone={log.filters.tz} />
			{:else}
				<p>{m.activity_empty()}</p>
			{/if}
		</svelte:boundary>
	{/if}
</div>

<style>
	/* The backend asks Gotenberg for A4 with 0.4in margins too; these win (preferCssPageSize). */
	@page {
		size: A4;
		margin: 0.4in;
	}
	.sheet {
		display: flex;
		flex-direction: column;
		gap: var(--space-lg);
		width: 180mm;
		margin: 0 auto;
		color: var(--color-ink);
	}
	p {
		margin: var(--space-xs) 0 0;
		font: var(--font-body-md);
		color: var(--color-body);
	}
	strong {
		color: var(--color-ink);
	}
</style>
