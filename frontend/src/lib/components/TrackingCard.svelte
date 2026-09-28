<!-- Shown once after a guest submits (FR-G2). The token is not stored anywhere else in the browser. -->
<script lang="ts">
	import QRCode from 'qrcode';
	import Alert from './Alert.svelte';
	import Button from './Button.svelte';
	import { showToast } from './toast-state.svelte';
	import { m } from '$lib/paraglide/messages';

	let { ticketId, token }: { ticketId: number; token: string } = $props();

	// Token after '#': browsers never send it to the server, so it stays out of access logs and Referer headers.
	const url = $derived(`${location.origin}/track#${token}`);
	let qr = $state('');
	$effect(() => {
		// SVG: sharp at any size and identical wherever it is generated.
		QRCode.toString(url, { type: 'svg', margin: 1 }).then((svg) => (qr = 'data:image/svg+xml,' + encodeURIComponent(svg)));
	});

	let input: HTMLInputElement;
	async function copy() {
		try {
			await navigator.clipboard.writeText(url);
		} catch {
			// Clipboard can be blocked on plain-HTTP pages; select the text so the guest can copy it by hand.
			input.select();
			return;
		}
		showToast(m.track_link_copied());
	}
</script>

<section class="card" aria-labelledby="tracking-title">
	<!-- tabindex: the report page moves focus here after submit. -->
	<h1 id="tracking-title" tabindex="-1">{m.form_created({ id: String(ticketId) })}</h1>
	<Alert>{m.track_save_warning()}</Alert>

	<label for="tracking-link">{m.track_link_label()}</label>
	<div class="row">
		<input id="tracking-link" bind:this={input} readonly value={url} onfocus={(e) => e.currentTarget.select()} />
		<Button variant="secondary" onclick={copy}>{m.track_copy()}</Button>
	</div>

	{#if qr}<img src={qr} width="200" height="200" alt={m.track_qr_alt()} />{/if}
</section>

<style>
	.card {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
		padding: var(--space-lg);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-xl);
		background: var(--color-canvas);
		box-shadow: var(--shadow-md);
	}
	label {
		font: var(--font-label);
		color: var(--color-ink);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-xs);
	}
	input {
		flex: 1 1 240px;
		min-width: 0;
		min-height: var(--size-control);
		padding: var(--space-sm);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-md);
		background: var(--color-surface-soft);
		font: var(--font-body-sm);
		color: var(--color-ink);
	}
	img {
		align-self: center;
		background: var(--color-canvas);
		border-radius: var(--radius-md);
	}
</style>
