import { getRoles, getStaffAccounts } from '$lib/api';
import type { PageLoad } from './$types';

// Without staff.manage the page shows "no permission"; the API refuses these calls anyway (FR-R3).
// Roles come along for the role selects and to spot Root Admin rows.
export const load: PageLoad = async ({ parent }) => {
	const { me } = await parent();
	if (!me.permissions.includes('staff.manage')) return { accounts: null, roles: [] };
	const [accounts, roles] = await Promise.all([getStaffAccounts(), getRoles()]);
	return { accounts, roles };
};
