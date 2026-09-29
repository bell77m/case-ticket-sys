import { ApiError, getActivity, getAssignees, getStaffAccounts, type ActivityPage, type Me } from '$lib/api';
import { m } from '$lib/paraglide/messages';
import { getLocale } from '$lib/paraglide/runtime';
import type { PageLoad } from './$types';

// Activity log (T3.06, FR-L1). Filters live in the URL: staff, action, ticket, from, to, page.
// Without audit.view the page shows "no permission"; the API refuses the call anyway (FR-R3).
export const load: PageLoad = async ({ parent, url }) => {
	const { me } = await parent();
	if (!me.permissions.includes('audit.view')) return { allowed: false as const };

	const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
	const api = new URLSearchParams({ lang: getLocale(), tz, page: url.searchParams.get('page') ?? '1' });
	for (const key of ['staff', 'action', 'ticket', 'from', 'to']) {
		const v = url.searchParams.get(key);
		if (v) api.set(key, v);
	}
	const [log, staff] = await Promise.all([
		getActivity(api).catch((err) => {
			if (err instanceof ApiError && err.code === 'validation') return err;
			throw err;
		}),
		staffOptions(me)
	]);
	return log instanceof ApiError
		? { allowed: true as const, log: null, fields: log.fields, staff }
		: { allowed: true as const, log: log as ActivityPage, fields: {} as Record<string, string>, staff };
};

/**
 * The staff filter lists what the viewer may read: every account with staff.manage (deactivated staff too, their
 * actions stay in the log), else active staff with ticket.assign (all default roles holding audit.view have it).
 * null = neither: the page asks for a staff ID instead.
 */
async function staffOptions(me: Me) {
	if (me.permissions.includes('staff.manage')) {
		const all = await getStaffAccounts();
		return [...all.filter((a) => a.is_active), ...all.filter((a) => !a.is_active)].map((a) => ({
			value: String(a.id),
			label: a.is_active ? a.name : m.activity_staff_inactive({ name: a.name })
		}));
	}
	if (me.permissions.includes('ticket.assign')) return (await getAssignees()).map((a) => ({ value: String(a.id), label: a.name }));
	return null;
}
