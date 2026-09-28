import { redirect } from '@sveltejs/kit';
import { ApiError, getMe, type Me } from '$lib/api';
import type { LayoutLoad } from './$types';

// Staff pages need a session. This only picks the page to show; the API checks every call (FR-R3).
export const load: LayoutLoad = async ({ url }) => {
	let me: Me;
	try {
		me = await getMe();
	} catch (err) {
		if (err instanceof ApiError && err.status === 401) redirect(307, '/login');
		throw err;
	}
	// FR-A8: a temporary password comes first. The API refuses other staff calls until then anyway.
	if (me.must_change_password && url.pathname !== '/staff/password') redirect(307, '/staff/password');
	return { me };
};
