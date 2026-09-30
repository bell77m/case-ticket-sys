import { ApiError, getBuildingOptions, getCategories, getReport, type Report } from '$lib/api';
import { getLocale } from '$lib/paraglide/runtime';
import type { PageLoad } from './$types';

// Reports dashboard (T3.03, FR-P1, FR-P2). Filters live in the URL: from, to, building (English name), category_id.
// Without report.view the page shows "no permission"; the API refuses the call anyway (FR-R3).
export const load: PageLoad = async ({ parent, url, depends }) => {
	depends('app:reports'); // live refresh (FR-P3)
	const { me } = await parent();
	if (!me.permissions.includes('report.view')) return { allowed: false as const };

	const lang = getLocale();
	const api = new URLSearchParams({ tz: Intl.DateTimeFormat().resolvedOptions().timeZone, lang });
	for (const key of ['from', 'to', 'building', 'category_id']) {
		const v = url.searchParams.get(key);
		if (v) api.set(key, v);
	}
	const [report, buildingOptions, categories] = await Promise.all([
		getReport(api).catch((err) => {
			if (err instanceof ApiError && err.code === 'validation') return null;
			throw err;
		}),
		getBuildingOptions(lang),
		getCategories(lang)
	]);
	return { allowed: true as const, report: report as Report | null, buildingOptions, categories };
};
