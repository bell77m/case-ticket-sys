// Typed calls to the Go API. Errors come back as codes; pages translate them (CLAUDE.md "i18n").

export type NamedItem = { id: number; name: string };
export type Floor = { name: string; lines: NamedItem[] };
export type Building = { name: string; floors: Floor[] };

export type NewTicket = {
	guest_name: string;
	employee_id: string;
	location_id: number;
	case_details: string;
	language: string;
};
export type CreatedTicket = { ticket_id: number; tracking_token: string };

/** An API error: `code` such as "validation" or "file.too_large", plus per-field codes for validation. */
export class ApiError extends Error {
	constructor(
		readonly status: number,
		readonly code: string,
		readonly fields: Record<string, string> = {}
	) {
		super(code);
	}
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
	const res = await fetch(path, init);
	const body = await res.json().catch(() => ({}));
	if (!res.ok) throw new ApiError(res.status, body.error ?? 'internal', body.fields);
	return body as T;
}

export const getLocations = (lang: string) =>
	call<Building[]>(`/api/locations?lang=${encodeURIComponent(lang)}`);

export const createTicket = (t: NewTicket) =>
	call<CreatedTicket>('/api/tickets', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(t)
	});

export function uploadAttachment(ticketId: number, token: string, file: File) {
	const form = new FormData();
	form.append('file', file);
	return call<{ id: number }>(`/api/tickets/${ticketId}/attachments`, {
		method: 'POST',
		headers: tokenHeader(token),
		body: form
	});
}

// Guest tracking (FR-G3). The token travels in a header, never in the URL path or query.
export type TrackView = {
	ticket_id: number;
	status: 'new' | 'in_progress' | 'waiting' | 'resolved' | 'closed';
	case_details: string;
	created_at: string;
	location: { building: string; floor: string; line: string };
	comments: { id: number; from: 'guest' | 'staff'; body: string; created_at: string }[];
	attachments: { id: number; media_type: 'image' | 'video'; size_bytes: number; created_at: string }[];
};
export type TrackComment = TrackView['comments'][number];

const tokenHeader = (token: string) => ({ 'X-Tracking-Token': token });

export const getTrack = (token: string, lang: string) =>
	call<TrackView>(`/api/track?lang=${encodeURIComponent(lang)}`, { headers: tokenHeader(token) });

export const replyTrack = (token: string, body: string) =>
	call<TrackComment>('/api/track/comments', {
		method: 'POST',
		headers: { ...tokenHeader(token), 'Content-Type': 'application/json' },
		body: JSON.stringify({ body })
	});

export const confirmTrack = (token: string) =>
	call<{ status: string }>('/api/track/confirm', { method: 'POST', headers: tokenHeader(token) });

/** Evidence needs the token header, so it is fetched as a blob instead of used as a plain <img src>. */
export async function trackFileURL(token: string, id: number) {
	const res = await fetch(`/api/track/attachments/${id}`, { headers: tokenHeader(token) });
	if (!res.ok) throw new ApiError(res.status, 'file.not_found');
	return URL.createObjectURL(await res.blob());
}

// Staff session (T2.15). The cookie is HttpOnly; these calls sign in and out and ask the API who is signed in.
export type Me = {
	id: number;
	name: string;
	username: string;
	role: string;
	language: string;
	permissions: string[];
	/** A temporary password must be replaced before anything else (FR-A8). */
	must_change_password: boolean;
};

export const getMe = () => call<Me>('/api/auth/me');

/** Errors: auth.invalid (any wrong username or password, FR-A2), auth.too_many_attempts (FR-A11). */
export const login = (username: string, password: string) =>
	call<{ must_change_password: boolean }>('/api/auth/login', sendJSON('POST', { username, password }));

/** Errors: validation with fields current_password (wrong) and new_password (required, too_short, too_long, same). */
export const changePassword = (current_password: string, new_password: string) =>
	call<void>('/api/auth/password', sendJSON('POST', { current_password, new_password }));

export const logout = () => call<unknown>('/api/auth/logout', { method: 'POST' });

// Staff queue (T1.18). Filters are repeated query parameters; see GET /api/staff/tickets.
export type QueueItem = {
	id: number;
	summary: string;
	status: 'new' | 'in_progress' | 'waiting' | 'resolved' | 'closed';
	priority: 'low' | 'medium' | 'high' | 'urgent' | null;
	category: string | null;
	location: { building: string; floor: string; line: string };
	guest_name: string;
	employee_id: string;
	assignee: NamedItem | null;
	created_at: string;
	updated_at: string;
};
export type QueuePage = { items: QueueItem[]; total: number; page: number; page_size: number };

export const getQueue = (params: URLSearchParams) => call<QueuePage>(`/api/staff/tickets?${params}`);

// Realtime (FR-P3): `new EventSource('/api/events')` gets an `event: ticket` with this data whenever a ticket
// changes. It carries no ticket content; refetch through the endpoints above.
export type TicketEvent = { type: 'ticket'; id: number };

// Staff ticket detail (T1.19). Evidence URLs need no header: the session cookie goes along.
export type StaffTicket = {
	id: number;
	summary: string;
	case_details: string;
	status: QueueItem['status'];
	priority: QueueItem['priority'];
	category: string | null;
	category_id: number | null;
	location: { building: string; floor: string; line: string };
	guest_name: string;
	employee_id: string;
	language: string;
	assignee: NamedItem | null;
	first_response_at: string | null;
	resolved_at: string | null;
	created_at: string;
	updated_at: string;
	comments: { id: number; author: NamedItem | null; body: string; internal: boolean; created_at: string }[];
	attachments: TrackView['attachments'];
	timeline: TimelineEvent[];
};

/**
 * One audit row of the ticket, oldest first (FR-L3). `from`/`to` are status or priority codes, staff names
 * (ticket.assigned), category names (ticket.category_changed) or "public"/"internal" (comment.added); absent when empty.
 */
export type TimelineEvent = {
	id: number;
	action: string;
	actor: { type: 'guest' | 'staff' | 'system'; name?: string };
	from?: string;
	to?: string;
	created_at: string;
};

export const getStaffTicket = (id: string, lang: string) =>
	call<StaffTicket>(`/api/staff/tickets/${encodeURIComponent(id)}?lang=${encodeURIComponent(lang)}`);

export const staffFileURL = (ticketId: number, fileId: number) => `/api/staff/tickets/${ticketId}/attachments/${fileId}`;

// Staff actions (T2.04, T2.05). Writes answer 204; call() turns the empty body into {}.
export type TicketPatch = { status?: StaffTicket['status']; priority?: NonNullable<StaffTicket['priority']>; category_id?: number };

const sendJSON = (method: string, body: unknown): RequestInit => ({
	method,
	headers: { 'Content-Type': 'application/json' },
	body: JSON.stringify(body)
});

export const getAssignees = () => call<NamedItem[]>('/api/staff/assignees');

export const getCategories = (lang: string) => call<NamedItem[]>(`/api/categories?lang=${encodeURIComponent(lang)}`);

export const updateTicket = (id: number, patch: TicketPatch) => call<void>(`/api/staff/tickets/${id}`, sendJSON('PATCH', patch));

export const assignTicket = (id: number, assigneeId: number | null) =>
	call<void>(`/api/staff/tickets/${id}/assignee`, sendJSON('PUT', { assignee_id: assigneeId }));

export const addStaffComment = (id: number, body: string, internal: boolean) =>
	call<StaffTicket['comments'][number]>(`/api/staff/tickets/${id}/comments`, sendJSON('POST', { body, internal }));

// Staff accounts and roles (T2.08, T2.09). Each call needs staff.manage; creating staff and changing roles are
// Root Admin only (FR-A1, FR-A3). Pages translate the error codes.
export type StaffAccount = {
	id: number;
	name: string;
	username: string;
	role: NamedItem;
	is_active: boolean;
	created_at: string;
};
/** `root` marks the Root Admin role (the one holding role.manage). */
export type Role = { id: number; name: string; root: boolean; permissions: string[]; staff_count: number };

export const getStaffAccounts = () => call<StaffAccount[]>('/api/staff/accounts');

/** Root Admin only (FR-A1). The password is temporary (FR-A8). Errors: validation, staff.username_taken. */
export const createStaffAccount = (a: { name: string; username: string; role_id: number; password: string }) =>
	call<StaffAccount>('/api/staff/accounts', sendJSON('POST', a));

/** Root Admin only (FR-A9): a new temporary password; the account's sessions end. Errors: validation, staff.not_found. */
export const resetStaffPassword = (id: number, password: string) =>
	call<void>(`/api/staff/accounts/${id}/password`, sendJSON('PUT', { password }));

export const updateStaffAccount = (id: number, patch: { role_id?: number; is_active?: boolean }) =>
	call<StaffAccount>(`/api/staff/accounts/${id}`, sendJSON('PATCH', patch));

export const getRoles = () => call<Role[]>('/api/staff/roles');

export const createRole = (name: string, permissions: string[]) =>
	call<Role>('/api/staff/roles', sendJSON('POST', { name, permissions }));

export const setRolePermissions = (id: number, permissions: string[]) =>
	call<Role>(`/api/staff/roles/${id}/permissions`, sendJSON('PUT', { permissions }));

// Categories and locations (T2.11). Each call needs category.manage. Every name is given in all four
// languages (FR-I4); rows are deactivated, never deleted. Errors: validation (fields such as "name.th" or
// "building.my"), category.exists, location.exists, category.not_found, location.not_found.
export type Names = { en: string; 'zh-CN': string; my: string; th: string };
export type AdminCategory = { id: number; name: Names; is_active: boolean };
/** One building / floor / line. To add a line under a floor, send that building's and floor's Names unchanged. */
export type AdminLocation = { id: number; building: Names; floor: Names; line: Names; is_active: boolean };
export type NewLocation = Pick<AdminLocation, 'building' | 'floor' | 'line'>;

export const getAdminCategories = () => call<AdminCategory[]>('/api/staff/categories');

export const createCategory = (name: Names) => call<AdminCategory>('/api/staff/categories', sendJSON('POST', { name }));

export const updateCategory = (id: number, patch: { name?: Names; is_active?: boolean }) =>
	call<AdminCategory>(`/api/staff/categories/${id}`, sendJSON('PATCH', patch));

export const getAdminLocations = () => call<AdminLocation[]>('/api/staff/locations');

export const createLocation = (l: NewLocation) => call<AdminLocation>('/api/staff/locations', sendJSON('POST', l));

export const updateLocation = (id: number, patch: Partial<NewLocation> & { is_active?: boolean }) =>
	call<AdminLocation>(`/api/staff/locations/${id}`, sendJSON('PATCH', patch));

// Reports dashboard (T3.01), report.view only. Params: from, to (YYYY-MM-DD, inclusive), tz (IANA), building (English
// name), category_id, lang. Open, unassigned and urgent_open are counted as of the period end; the rest inside it.
// Medians are seconds, null when no ticket qualifies.
export type ReportCard<T = number> = { value: T; previous: T };
export type Report = {
	period: { from: string; to: string; previous_from: string; previous_to: string };
	cards: {
		open: ReportCard;
		unassigned: ReportCard;
		urgent_open: ReportCard;
		new: ReportCard;
		resolved: ReportCard;
		first_response_median_seconds: ReportCard<number | null>;
		resolution_median_seconds: ReportCard<number | null>;
	};
	opened_resolved_daily: { date: string; opened: number; resolved: number }[];
	open_by_status: { status: QueueItem['status']; count: number }[];
	/** priority null = not triaged yet */
	open_by_priority: { priority: QueueItem['priority']; count: number }[];
	/** Most tickets first; id and name null = no category. */
	by_category: { id: number | null; name: string | null; count: number }[];
	/** Most tickets first. */
	by_location: {
		building: string;
		count: number;
		floors: { floor: string; count: number; lines: { id: number; line: string; count: number }[] }[];
	}[];
	workload: { assignee: NamedItem; urgent: number; high: number; medium: number; low: number; none: number }[];
	weekly_medians: { week_start: string; first_response_seconds: number | null; resolution_seconds: number | null }[];
};

export const getReport = (params: URLSearchParams) => call<Report>(`/api/staff/reports?${params}`);
