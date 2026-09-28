// Realtime ticket events (FR-P3): one EventSource per tab, opened for the first subscriber, closed after the last.
import { navigating } from '$app/state';
import { getMe, type TicketEvent } from '$lib/api';

type Listener = (e: TicketEvent) => void;
const listeners = new Set<Listener>();
let source: EventSource | undefined;
let dropped = false; // the stream broke; changes made until it is back were missed

const emit = (e: TicketEvent) => listeners.forEach((cb) => cb(e));

function connect() {
	const es = new EventSource('/api/events');
	source = es;
	es.addEventListener('ticket', (msg) => emit(JSON.parse(msg.data) as TicketEvent));
	// Back after a drop (pod restart, proxy timeout): id 0 tells every page to refetch what it shows.
	es.addEventListener('open', () => {
		if (dropped) emit({ type: 'ticket', id: 0 });
		dropped = false;
	});
	es.addEventListener('error', () => (dropped = true));
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
 * Wraps a page refresh for bursts of events: the first runs it at once, the rest of the next `ms` collapse into one
 * more run at its end. At most one run per `ms`, never starved. Skipped while a navigation is under way: its page
 * loads fresh data, and SvelteKit lets an invalidate cancel the navigation.
 */
export function coalesce(fn: () => void, ms = 300) {
	let timer: ReturnType<typeof setTimeout> | undefined;
	let queued = false;
	const run = () => {
		if (!navigating.to) fn();
		timer = setTimeout(() => {
			timer = undefined;
			if (queued) {
				queued = false;
				run();
			}
		}, ms);
	};
	return () => {
		if (timer) queued = true;
		else run();
	};
}
