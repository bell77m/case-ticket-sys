<script lang="ts" module>
	import { m } from '$lib/paraglide/messages';

	export type Status = 'new' | 'in_progress' | 'waiting' | 'resolved' | 'closed';
	export type Priority = 'low' | 'medium' | 'high' | 'urgent';
	export const statuses: Status[] = ['new', 'in_progress', 'waiting', 'resolved', 'closed'];
	export const priorities: Priority[] = ['urgent', 'high', 'medium', 'low'];

	// Enum codes come from the API; labels are translated here only (CLAUDE.md "Enums").
	export const labels: Record<Status | Priority, () => string> = {
		new: m.status_new,
		in_progress: m.status_in_progress,
		waiting: m.status_waiting,
		resolved: m.status_resolved,
		closed: m.status_closed,
		low: m.priority_low,
		medium: m.priority_medium,
		high: m.priority_high,
		urgent: m.priority_urgent
	};
</script>

<script lang="ts">
	type Props = { kind: 'status'; value: Status } | { kind: 'priority'; value: Priority };
	let { kind, value }: Props = $props();
</script>

<span class="badge {kind}-{value}">
	{#if value !== 'urgent'}<span class="dot" aria-hidden="true"></span>{/if}
	{labels[value]()}
</span>

<style>
	.badge {
		display: inline-flex;
		align-items: center;
		gap: var(--space-xs);
		padding: var(--space-xxs) var(--space-sm);
		border-radius: var(--radius-pill);
		font: var(--font-caption);
		color: var(--color-ink);
		white-space: nowrap;
	}
	.dot {
		width: var(--space-xs);
		height: var(--space-xs);
		border-radius: var(--radius-full);
		background: var(--dot);
	}
	.status-new,
	.priority-medium {
		background: var(--color-accent-tint);
		--dot: var(--color-brand-accent);
	}
	.status-in_progress,
	.priority-high {
		background: var(--color-warning-tint);
		--dot: var(--color-warning);
	}
	.status-waiting {
		background: var(--color-violet-tint);
		--dot: var(--color-badge-violet);
	}
	.status-resolved {
		background: var(--color-success-tint);
		--dot: var(--color-success);
	}
	.status-closed {
		background: var(--color-surface-card);
		--dot: var(--color-muted-soft);
	}
	.priority-low {
		background: var(--color-surface-card);
		--dot: var(--color-muted);
	}
	.priority-urgent {
		background: var(--color-error-strong);
		color: var(--color-on-primary);
	}
</style>
