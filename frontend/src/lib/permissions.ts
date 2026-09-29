import { m } from './paraglide/messages';

// Permission codes in the order of rbac.All (backend/internal/rbac/rbac.go). Codes are translated here only.
export const permissions: [string, () => string][] = [
	['ticket.view_all', m.perm_ticket_view_all],
	['ticket.comment', m.perm_ticket_comment],
	['ticket.update', m.perm_ticket_update],
	['ticket.assign', m.perm_ticket_assign],
	['ticket.delete', m.perm_ticket_delete],
	['report.view', m.perm_report_view],
	['audit.view', m.perm_audit_view],
	['category.manage', m.perm_category_manage],
	['staff.manage', m.perm_staff_manage],
	['staff.create', m.perm_staff_create],
	['role.manage', m.perm_role_manage]
];

const byCode = new Map(permissions);

/** The translated label of a permission code; an unknown code shows as itself. */
export const permissionLabel = (code: string) => byCode.get(code)?.() ?? code;
