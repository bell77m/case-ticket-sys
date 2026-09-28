import { getAdminCategories, getAdminLocations } from '$lib/api';
import type { PageLoad } from './$types';

// Without category.manage the page shows "no permission"; the API refuses these calls anyway (FR-R3).
export const load: PageLoad = async ({ parent }) => {
	const { me } = await parent();
	if (!me.permissions.includes('category.manage')) return { categories: null, locations: [] };
	const [categories, locations] = await Promise.all([getAdminCategories(), getAdminLocations()]);
	return { categories, locations };
};
