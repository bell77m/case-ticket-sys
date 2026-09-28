import { getRoles } from '$lib/api';
import type { PageLoad } from './$types';

// Without staff.manage the page shows "no permission"; the API refuses the call anyway (FR-R3).
export const load: PageLoad = async ({ parent }) => {
	const { me } = await parent();
	return { roles: me.permissions.includes('staff.manage') ? await getRoles() : null };
};
