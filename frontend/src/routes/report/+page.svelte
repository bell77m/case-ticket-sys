<!-- Guest ticket form (T1.13, FR-G1, FR-T1). No login; staff set category and priority later (FR-T3). -->
<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { ApiError, createTicket, getLocations, uploadAttachment, type Building } from '$lib/api';
	import Alert from '$lib/components/Alert.svelte';
	import Button from '$lib/components/Button.svelte';
	import FileDrop from '$lib/components/FileDrop.svelte';
	import LanguageSwitcher from '$lib/LanguageSwitcher.svelte';
	import Select from '$lib/components/Select.svelte';
	import TextArea from '$lib/components/TextArea.svelte';
	import TextInput from '$lib/components/TextInput.svelte';
	import TrackingCard from '$lib/components/TrackingCard.svelte';
	import { m } from '$lib/paraglide/messages';
	import { getLocale } from '$lib/paraglide/runtime';

	let buildings = $state<Building[]>([]);
	let guestName = $state('');
	let employeeId = $state('');
	let building = $state('');
	let floor = $state('');
	let line = $state('');
	let details = $state('');
	let files = $state<File[]>([]);

	let errors = $state<Record<string, string>>({});
	let formError = $state('');
	let sending = $state(false);
	let result = $state<{ id: number; token: string; uploaded: number; failed: string[] } | null>(null);

	const floors = $derived(building === '' ? [] : buildings[+building].floors);
	const lines = $derived(floor === '' ? [] : floors[+floor].lines);

	onMount(async () => {
		try {
			buildings = await getLocations(getLocale());
		} catch {
			formError = m.err_network();
		}
	});

	// Field error codes from the API or local checks, shown in the guest's language.
	const errorText: Record<string, () => string> = {
		required: m.err_required,
		too_short: m.err_too_short,
		too_long: m.err_too_long,
		invalid_characters: m.err_invalid_characters,
		not_found: m.err_required
	};
	// The location error sits on the first dropdown still to choose: building, then floor, then line.
	const locationLevel = $derived(building === '' ? 'building' : floor === '' ? 'floor' : 'line');
	const locationError = (level: string) => (line === '' && level === locationLevel ? fieldError('location_id') : undefined);
	const fieldError = (name: string) => (errors[name] ? (errorText[errors[name]] ?? m.err_required)() : undefined);

	function localErrors() {
		const e: Record<string, string> = {};
		if (!guestName.trim()) e.guest_name = 'required';
		if (!employeeId.trim()) e.employee_id = 'required';
		if (line === '') e.location_id = 'required';
		const n = [...details.trim()].length;
		if (n === 0) e.case_details = 'required';
		else if (n < 10) e.case_details = 'too_short';
		else if (n > 5000) e.case_details = 'too_long';
		return e;
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		formError = '';
		errors = localErrors();
		if (Object.keys(errors).length) return;

		sending = true;
		try {
			const t = await createTicket({
				guest_name: guestName,
				employee_id: employeeId,
				location_id: +line,
				case_details: details,
				language: getLocale()
			});
			// One file per request (T1.12); a failed file does not undo the ticket.
			const failed: string[] = [];
			for (const f of files) {
				try {
					await uploadAttachment(t.ticket_id, t.tracking_token, f);
				} catch {
					failed.push(f.name);
				}
			}
			result = { id: t.ticket_id, token: t.tracking_token, uploaded: files.length - failed.length, failed };
			await tick();
			document.getElementById('tracking-title')?.focus();
		} catch (err) {
			// Form values stay, so the guest can resend after waiting (NFR-3).
			if (!(err instanceof ApiError)) formError = m.err_network();
			else if (err.code === 'validation') errors = err.fields;
			else if (err.code === 'ticket.rate_limited') formError = m.report_err_rate_limited();
			else formError = m.detail_err_failed();
		} finally {
			sending = false;
		}
	}

	// A clean form for the next problem; nothing from the last ticket stays on screen.
	function reportAnother() {
		guestName = employeeId = building = floor = line = details = '';
		files = [];
		errors = {};
		result = null;
		scrollTo(0, 0);
	}
</script>

<!-- Replaces the layout's tools row here: back link left, language picker right, both over the form column. -->
<div class="bar rise">
	{#if !result}
		<a class="back" href="/">
			<span class="arrow" aria-hidden="true">
				<svg viewBox="0 0 24 24"><path d="M19 12H5m7 7-7-7 7-7" /></svg>
			</span>
			{m.success_home()}
		</a>
	{/if}
	<LanguageSwitcher />
</div>

{#if result}
	<section class="done rise">
		<svg class="check" viewBox="0 0 52 52" aria-hidden="true">
			<circle cx="26" cy="26" r="24" />
			<path d="m15 27 7 7 15-15" pathLength="1" />
		</svg>
		<TrackingCard ticketId={result.id} token={result.token} />
		{#if result.uploaded > 0}
			<p>{result.uploaded === 1 ? m.form_file_attached_one() : m.form_files_attached({ count: String(result.uploaded) })}</p>
		{/if}
		{#each result.failed as name (name)}
			<Alert variant="warning">{m.err_upload_failed({ name })}</Alert>
		{/each}
		<div class="next">
			<Button onclick={reportAnother}>{m.success_another()}</Button>
			<Button variant="secondary" href="/">{m.success_home()}</Button>
		</div>
	</section>
{:else}
	<form class="rise" onsubmit={submit} novalidate>
		<h1>{m.home_heading()}</h1>
		{#if formError}<Alert variant="error">{formError}</Alert>{/if}

		<TextInput label={m.form_name()} bind:value={guestName} maxlength={100} autocomplete="name" error={fieldError('guest_name')} />
		<TextInput label={m.form_employee_id()} bind:value={employeeId} maxlength={20} error={fieldError('employee_id')} />

		<Select
			label={m.form_building()}
			placeholder={m.form_choose_building()}
			options={buildings.map((b, i) => ({ value: String(i), label: b.name }))}
			bind:value={building}
			onchange={() => ((floor = ''), (line = ''))}
			error={locationError('building')}
		/>
		<Select
			label={m.form_floor()}
			placeholder={building === '' ? m.form_choose_building_first() : m.form_choose_floor()}
			options={floors.map((f, i) => ({ value: String(i), label: f.name }))}
			bind:value={floor}
			onchange={() => (line = '')}
			disabled={building === ''}
			error={locationError('floor')}
		/>
		<Select
			label={m.form_line()}
			placeholder={floor === '' ? m.form_choose_floor_first() : m.form_choose_line()}
			options={lines.map((l) => ({ value: String(l.id), label: l.name }))}
			bind:value={line}
			disabled={floor === ''}
			error={locationError('line')}
		/>

		<TextArea
			label={m.form_details()}
			helper={m.form_details_helper()}
			bind:value={details}
			maxlength={5000}
			error={fieldError('case_details')}
		/>

		<FileDrop bind:files />

		<div class="actions">
			<Button type="submit" disabled={sending}>{sending ? m.form_submitting() : m.form_submit()}</Button>
		</div>
	</form>
{/if}

<style>
	form,
	.done {
		display: flex;
		flex-direction: column;
		gap: var(--space-lg);
		max-width: 640px;
		margin: 0 auto;
	}
	/* Tablet and up: the form sits in a card (DESIGN.md "Forms"). */
	@media (min-width: 768px) {
		form {
			padding: var(--space-xl);
			border: 1px solid var(--color-hairline);
			border-radius: var(--radius-xl);
			box-shadow: var(--shadow-md);
		}
	}
	.bar {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		align-items: center;
		gap: var(--space-xs);
		max-width: 640px;
		margin: 0 auto var(--space-md);
	}
	/* Back link: round arrow (button-icon-circular) plus a muted label. */
	.back {
		display: flex;
		align-items: center;
		gap: var(--space-xs);
		min-height: var(--size-control);
		margin-right: auto;
		font: var(--font-nav-link);
		color: var(--color-muted);
		text-decoration: none;
		transition: color var(--motion-fast);
	}
	.back:hover {
		color: var(--color-ink);
	}
	.arrow {
		display: grid;
		place-items: center;
		width: var(--size-icon-button);
		height: var(--size-icon-button);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-full);
		background: var(--color-canvas);
		box-shadow: var(--shadow-sm);
		transition: transform var(--motion-fast);
	}
	.back:hover .arrow {
		transform: translateX(calc(-1 * var(--space-xxs)));
	}
	.arrow svg {
		width: var(--space-md);
		height: var(--space-md);
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.actions {
		display: flex;
		justify-content: flex-end;
	}
	/* Success: the circle pops in, then the tick draws itself. */
	.check {
		align-self: center;
		width: calc(2 * var(--space-xl));
		height: calc(2 * var(--space-xl));
		animation: pop var(--motion-enter) both;
	}
	.check circle {
		fill: var(--color-success-tint);
		stroke: var(--color-success);
		stroke-width: 2;
	}
	.check path {
		fill: none;
		stroke: var(--color-success);
		stroke-width: 4;
		stroke-linecap: round;
		stroke-linejoin: round;
		stroke-dasharray: 1;
		animation: draw var(--motion-enter) 200ms both;
	}
	@keyframes pop {
		from {
			transform: scale(0.5);
			opacity: 0;
		}
	}
	@keyframes draw {
		from {
			stroke-dashoffset: 1;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.check,
		.check path {
			animation: none;
		}
	}
	.next {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-sm);
	}
	@media (max-width: 767px) {
		.next {
			flex-direction: column;
		}
	}
	/* Phones: keep the submit button in reach at the bottom of the screen (DESIGN.md "Forms"). */
	@media (max-width: 767px) {
		.actions {
			position: sticky;
			bottom: 0;
			margin: 0 calc(-1 * var(--space-md));
			padding: var(--space-sm) var(--space-md);
			border-top: 1px solid var(--color-hairline);
			background: var(--color-canvas);
		}
		.actions :global(button) {
			width: 100%;
		}
	}
</style>
