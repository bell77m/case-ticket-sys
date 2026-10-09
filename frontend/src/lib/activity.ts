// Activity log (T3.06, FR-L1): how an audit row reads, shared by /staff/admin/activity and /print/activity.
// Action, status, priority and permission codes come from the API and are translated here only (CLAUDE.md "Enums").
import type { ActivityItem } from './api';
import { labels } from './components/Badge.svelte';
import { m } from './paraglide/messages';
import { getLocale } from './paraglide/runtime';
import { permissionLabel } from './permissions';

/** Every action code the API writes (FR-L1), in the order the action filter lists them. */
export const actionLabels: Record<string, () => string> = {
	'ticket.created': m.activity_action_ticket_created,
	'ticket.status_changed': m.activity_action_ticket_status_changed,
	'ticket.priority_changed': m.activity_action_ticket_priority_changed,
	'ticket.category_changed': m.activity_action_ticket_category_changed,
	'ticket.assigned': m.activity_action_ticket_assigned,
	'ticket.auto_closed': m.activity_action_ticket_auto_closed,
	'comment.added': m.activity_action_comment_added,
	'attachment.added': m.activity_action_attachment_added,
	'attachment.rejected': m.activity_action_attachment_rejected, // detail: the YARA rule's name (NFR-5)
	'category.created': m.activity_action_category_created,
	'category.changed': m.activity_action_category_changed,
	'category.deactivated': m.activity_action_category_deactivated,
	'category.reactivated': m.activity_action_category_reactivated,
	'location.created': m.activity_action_location_created,
	'location.changed': m.activity_action_location_changed,
	'location.deactivated': m.activity_action_location_deactivated,
	'location.reactivated': m.activity_action_location_reactivated,
	'login.success': m.activity_action_login_success,
	'login.failed': m.activity_action_login_failed,
	'staff.created': m.activity_action_staff_created,
	'staff.role_changed': m.activity_action_staff_role_changed,
	'staff.deactivated': m.activity_action_staff_deactivated,
	'staff.reactivated': m.activity_action_staff_reactivated,
	'staff.password_changed': m.activity_action_staff_password_changed,
	'staff.password_reset': m.activity_action_staff_password_reset,
	'role.created': m.activity_action_role_created,
	'role.changed': m.activity_action_role_changed,
	'report.exported': m.activity_action_report_exported,
	'activity.exported': m.activity_action_activity_exported
};

/** An unknown code shows as itself. */
export const actionLabel = (code: string) => actionLabels[code]?.() ?? code;

export const actorName = (a: ActivityItem['actor']) =>
	a.type === 'staff' ? (a.name ?? String(a.id)) : a.type === 'guest' ? m.detail_guest() : m.timeline_system();

const codeLabel = (v: string) => (v ? (labels[v as keyof typeof labels]?.() ?? v) : m.detail_not_set());
const permList = (v: string) => v.split(',').map(permissionLabel).join(', ');
const fileSize = (bytes: number) => {
	const unit = bytes < 1024 * 1024 ? 'kilobyte' : 'megabyte';
	const n = bytes / (unit === 'kilobyte' ? 1024 : 1024 * 1024);
	return new Intl.NumberFormat(getLocale(), { style: 'unit', unit, maximumFractionDigits: 1 }).format(n);
};

/** The row's old and new value ("New → In Progress"), or '' when it has none worth showing. */
export function change({ action, from = '', to = '' }: ActivityItem): string {
	switch (action) {
		case 'ticket.created': // "location:<id>"; the ticket shows its location
			return '';
		case 'ticket.status_changed':
		case 'ticket.priority_changed':
		case 'ticket.auto_closed':
			return `${codeLabel(from)} → ${codeLabel(to)}`;
		case 'ticket.category_changed':
			return `${from || m.detail_not_set()} → ${to || m.detail_not_set()}`;
		case 'ticket.assigned':
			return `${from || m.queue_assignee_none()} → ${to || m.queue_assignee_none()}`;
		case 'comment.added': // empty for a guest's reply
			return to === 'internal' ? m.detail_internal() : to === 'public' ? m.detail_reply_public() : '';
		case 'attachment.added': // size in bytes
			return to ? fileSize(Number(to)) : '';
		case 'role.created':
			return permList(to);
		case 'role.changed': // permissions taken away, then given
			return [from && m.activity_perms_removed({ list: permList(from) }), to && m.activity_perms_added({ list: permList(to) })]
				.filter(Boolean)
				.join(' · ');
	}
	return from && to ? `${from} → ${to}` : from || to;
}
