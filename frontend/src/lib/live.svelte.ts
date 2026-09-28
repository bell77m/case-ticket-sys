// Realtime ticket events (FR-P3): one EventSource per tab, opened for the first subscriber, closed after the last.
import { navigating } from '$app/state';
import { getMe, type TicketEvent } from '$lib/api';

type Listener = (e: TicketEvent) => void;
const listeners = new Set<Listener>();
let source: EventSource | undefined;

function connect() {
	const es = new EventSource('/api/events');
	source = es;
	es.addEventListener('ticket', (msg) => {
		const e = JSON.parse(msg.data) as TicketEvent;
		for (const cb of listeners) cb(e);
	});
	// A dropped stream reconnects by itself (readyState CONNECTING). A non-200 answer, such as 401 once the session is
	// gone or 403 without ticket.view_all, closes it for good, and EventSource cannot tell which: ask the API once.
	// Still allowed: try again shortly. Otherwise stay closed; the next page that subscribes tries again.
	es.addEventListener('error', async () => {
		if (es.readyState !== EventSource.CLOSED) return;
		const me = await getMe().catch(() => null);
		if (source !== es) return; // unsubscribed meanwhile
		source = undefined;
		if (me?.permissions.includes('ticket.view_all')) setTimeout(() => !source && listeners.size && connect(), 3000);
	});
}

/** Calls cb for every ticket change on any pod. Returns the unsubscribe function; call it on destroy. */
export function onTicketEvent(cb: Listener): () => void {
	listeners.add(cb);
	if (!source) connect();
	return () => {
		listeners.delete(cb);
		if (listeners.size) return;
		source?.close();
		source = undefined;
	};
}

/**
 * Wraps a page refresh so a burst of events runs it once, `ms` after the first: at most one run per `ms`, never starved.
 * Skipped while a navigation is under way: its page loads fresh data, and SvelteKit lets an invalidate cancel it.
 */
export function coalesce(fn: () => void, ms = 300) {
	let timer: ReturnType<typeof setTimeout> | undefined;
	return () => {
		timer ??= setTimeout(() => {
			timer = undefined;
			if (!navigating.to) fn();
		}, ms);
	};
}
