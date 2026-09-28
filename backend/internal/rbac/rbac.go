// Package rbac lists the staff permissions. They are fixed in code (FR-R2); roles pick from them.
package rbac

const (
	TicketViewAll  = "ticket.view_all"
	TicketComment  = "ticket.comment"
	TicketUpdate   = "ticket.update"
	TicketAssign   = "ticket.assign"
	TicketDelete   = "ticket.delete"
	ReportView     = "report.view"
	AuditView      = "audit.view"
	CategoryManage = "category.manage"
	StaffManage    = "staff.manage"
	StaffCreate    = "staff.create"
	RoleManage     = "role.manage"
)

// All is every permission, in the order of the table in docs/REQUIREMENTS.md.
var All = []string{
	TicketViewAll, TicketComment, TicketUpdate, TicketAssign, TicketDelete,
	ReportView, AuditView, CategoryManage, StaffManage, StaffCreate, RoleManage,
}
