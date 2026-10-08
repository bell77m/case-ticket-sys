<!-- Admin pages share the back link and a tab row between them. Layout: DESIGN.md "Admin tabs". -->
<script lang="ts">
	import { page } from '$app/state';
	import { m } from '$lib/paraglide/messages';

	let { data, children } = $props();

	// Same order and permissions as the account menu links. Display only: the API checks every call (FR-R3).
	const tabs = $derived(
		(
			[
				['/staff/admin/activity', 'audit.view', m.activity_heading],
				['/staff/admin/staff', 'staff.manage', m.staff_heading],
				['/staff/admin/roles', 'staff.manage', m.roles_heading],
				['/staff/admin/lookups', 'category.manage', m.lookups_heading]
			] as const
		).filter(([, p]) => data.me.permissions.includes(p))
	);
</script>

<a class="back" href="/staff">{m.detail_back()}</a>

<!-- One tab alone switches nowhere. -->
{#if tabs.length > 1}
	<nav aria-label={m.admin_nav()}>
		{#each tabs as [href, , label] (href)}
			<a {href} aria-current={page.url.pathname === href ? 'page' : undefined}>{label()}</a>
		{/each}
	</nav>
{/if}

{@render children()}

<style>
	.back {
		display: inline-block;
		margin-bottom: var(--space-md);
		font: var(--font-label);
		color: var(--color-ink);
	}
	/* nav-pill-group: tabs wrap on phones (long Burmese labels), so the group takes rounded.lg, not a pill. */
	nav {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-xxs);
		width: fit-content;
		max-width: 100%;
		margin-bottom: var(--space-lg);
		padding: var(--space-xxs);
		border-radius: var(--radius-lg);
		background: var(--color-surface-soft);
	}
	nav a {
		display: inline-flex;
		align-items: center;
		min-height: var(--size-control);
		padding: var(--space-xs) var(--space-sm);
		border-radius: var(--radius-md);
		font: var(--font-nav-link);
		color: var(--color-muted);
		text-decoration: none;
		transition: background-color var(--motion-fast);
	}
	nav a[aria-current='page'] {
		background: var(--color-canvas);
		color: var(--color-ink);
		box-shadow: var(--shadow-sm);
	}
</style>
