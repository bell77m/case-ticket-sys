import { getQueue } from '$lib/api';
import { getLocale } from '$lib/paraglide/runtime';
import type { PageLoad } from './$types';

// Page filters live in the URL (so other pages can link to a filtered queue) and map to API parameters.
// status: "open" (default) = new, in progress, waiting; "all" = no status filter; else one status code.
export const load: PageLoad = async ({ url, depends }) => {
	depends('app:queue'); // live refresh (FR-P3)
	const p = url.searchParams;
	const api = new URLSearchParams({ lang: getLocale(), page: p.get('page') ?? '1' });
	const status = p.get('status') ?? 'open';
	const statuses = status === 'open' ? ['new', 'in_progress', 'waiting'] : status === 'all' ? [] : [status];
	for (const s of statuses) api.append('status', s);
	for (const key of ['priority', 'assignee', 'q']) {
		const v = p.get(key);
		if (v) api.set(key, v);
	}
	return { queue: await getQueue(api) };
};
