// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
import type { Me } from '$lib/api';

declare global {
	namespace App {
		// interface Error {}
		// interface Locals {}
		interface PageData {
			me?: Me; // staff pages only (routes/staff/+layout.ts); the root layout's nav reads it
		}
		// interface PageState {}
		// interface Platform {}
	}
	interface Window {
		/** Set by the /print/ pages once drawn with their fonts; Gotenberg prints then (FR-P4). */
		printReady?: boolean;
	}
}

export {};
