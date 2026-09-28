import { dev } from '$app/environment';
import { error } from '@sveltejs/kit';

// Component gallery for development only; production builds return 404.
export const load = () => {
	if (!dev && !import.meta.env.VITE_SHOW_DEV_PAGES) error(404);
};
