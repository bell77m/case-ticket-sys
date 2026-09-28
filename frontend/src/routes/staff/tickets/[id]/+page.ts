import { ApiError, getAssignees, getCategories, getStaffTicket } from '$lib/api';
import { getLocale } from '$lib/paraglide/runtime';
import type { PageLoad } from './$types';

// Pick lists load only for staff who may use them; the API checks every write again (FR-R3).
export const load: PageLoad = async ({ params, parent, depends }) => {
	depends('app:ticket'); // live refresh (FR-P3)
	const { me } = await parent();
	const can = (p: string) => me.permissions.includes(p);
	const lang = getLocale();
	try {
		const [ticket, categories, assignees] = await Promise.all([
			getStaffTicket(params.id, lang),
			can('ticket.update') ? getCategories(lang) : [],
			can('ticket.assign') ? getAssignees() : []
		]);
		return { ticket, categories, assignees };
	} catch (err) {
		if (err instanceof ApiError && err.status === 404) return { ticket: null, categories: [], assignees: [] };
		throw err;
	}
};
