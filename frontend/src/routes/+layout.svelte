<script lang="ts">
	import '$lib/styles/tokens.css';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { logout } from '$lib/api';
	import LanguageSwitcher from '$lib/LanguageSwitcher.svelte';
	import Button from '$lib/components/Button.svelte';
	import Toast from '$lib/components/Toast.svelte';
	import { getLocale } from '$lib/paraglide/runtime';
	import { m } from '$lib/paraglide/messages';

	let { children } = $props();

	// Set by routes/staff/+layout.ts; guest pages have no signed-in staff member.
	const me = $derived(page.data.me);
	let menu = $state<HTMLDivElement>();

	// Per-language typography rules in tokens.css key off :lang(), so <html lang> must match the locale.
	$effect(() => {
		document.documentElement.lang = getLocale();
	});

	async function signOut() {
		try {
			await logout();
		} finally {
			await goto('/login');
		}
	}
</script>

<svelte:head>
	<title>{m.app_title()}</title>
</svelte:head>

<main>
	<!-- No top bar: the language picker (FR-I2) and, for staff, the account menu sit top right on every page and scroll away with it. -->
	<!-- /report puts the language picker in its own row, next to the back link and over the form column. -->
	{#if page.url.pathname !== '/report'}
		<div class="tools">
			<LanguageSwitcher />
			{#if me}
				<!-- Native popover: Esc and a click outside close it, focus returns to the button. -->
				<button class="account" popovertarget="account-menu">
					<span class="avatar" aria-hidden="true">
						<svg viewBox="0 0 24 24"><circle cx="12" cy="8" r="4" /><path d="M20 21a8 8 0 0 0-16 0" /></svg>
					</span>
					<span class="name">{me.name}</span>
					<svg class="chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
				</button>
				<div id="account-menu" class="menu" popover bind:this={menu}>
					<p class="menu-name">{me.name}</p>
					<p class="menu-username">{me.username}</p>
					<span class="role">{me.role}</span>
					<!-- Reports with report.view (T3.03); admin pages: staff and roles with staff.manage (T2.10), lookups with category.manage (T2.11). Display only: the API checks every call (FR-R3). -->
					{#if me.permissions.includes('report.view')}
						<a href="/staff/reports" onclick={() => menu?.hidePopover()}>{m.reports_heading()}</a>
					{/if}
					{#if me.permissions.includes('staff.manage')}
						<a href="/staff/admin/staff" onclick={() => menu?.hidePopover()}>{m.staff_heading()}</a>
						<a href="/staff/admin/roles" onclick={() => menu?.hidePopover()}>{m.roles_heading()}</a>
					{/if}
					{#if me.permissions.includes('category.manage')}
						<a href="/staff/admin/lookups" onclick={() => menu?.hidePopover()}>{m.lookups_heading()}</a>
					{/if}
					<a href="/staff/password" onclick={() => menu?.hidePopover()}>{m.password_heading()}</a>
					<Button variant="secondary" onclick={signOut}>{m.staff_sign_out()}</Button>
				</div>
			{/if}
		</div>
	{/if}
	{@render children()}
</main>

<Toast />

<style>
	svg {
		flex: none;
		width: var(--space-md);
		height: var(--space-md);
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.tools {
		display: flex;
		justify-content: flex-end;
		align-items: center;
		gap: var(--space-xs);
		margin-bottom: var(--space-md);
	}
	.account {
		anchor-name: --account;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-xs);
		min-width: var(--size-control);
		min-height: var(--size-control);
		padding: var(--space-xxs);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-pill);
		background: var(--color-canvas);
		color: var(--color-ink);
		font: var(--font-nav-link);
		cursor: pointer;
	}
	.account:active {
		background: var(--color-surface-soft);
	}
	.avatar {
		display: grid;
		place-items: center;
		width: var(--space-xl);
		height: var(--space-xl);
		border-radius: var(--radius-full);
		background: var(--color-surface-card);
	}
	.name {
		max-width: 16em;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.chevron {
		margin-right: var(--space-xs);
		color: var(--color-body);
	}
	/* Phones: the avatar alone; the name stays in the button's accessible name. */
	@media (max-width: 767px) {
		.name {
			position: absolute;
			width: 1px;
			height: 1px;
			clip-path: inset(50%);
		}
		.chevron {
			display: none;
		}
	}
	.menu {
		position-anchor: --account;
		position-area: bottom span-left;
		inset: auto;
		min-width: 240px;
		max-width: calc(100vw - 2 * var(--space-md));
		margin: var(--space-xs) 0 0;
		padding: var(--space-md);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
		background: var(--color-canvas);
		box-shadow: var(--shadow-md);
		color: var(--color-body);
	}
	/* Only when open: a display value on .menu itself would override the closed popover's display: none. */
	.menu:popover-open {
		display: flex;
		flex-direction: column;
		gap: var(--space-xxs);
	}
	.menu p {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.menu-name {
		font: var(--font-title-sm);
		color: var(--color-ink);
	}
	.menu-username {
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	/* 44px rows, text in line with the name above. */
	.menu a {
		display: flex;
		align-items: center;
		min-height: var(--size-control);
		margin: 0 calc(-1 * var(--space-sm));
		padding: 0 var(--space-sm);
		border-radius: var(--radius-md);
		color: var(--color-ink);
		font: var(--font-nav-link);
		text-decoration: none;
	}
	.menu a:active {
		background: var(--color-surface-card);
	}
	.menu a:last-of-type {
		margin-bottom: var(--space-sm);
	}
	.role {
		align-self: flex-start;
		margin: var(--space-xxs) 0 var(--space-sm);
		padding: var(--space-xxs) var(--space-sm);
		border-radius: var(--radius-pill);
		background: var(--color-surface-card);
		color: var(--color-ink);
		font: var(--font-caption);
	}
	main {
		max-width: 1440px;
		margin: 0 auto;
		padding: var(--space-lg) var(--space-md);
	}
	@media (min-width: 768px) {
		main {
			padding: var(--space-xxl) var(--space-lg);
		}
	}
</style>
