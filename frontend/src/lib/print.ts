// Print pages (FR-P4): what Gotenberg prints to PDF (/print/report, /print/activity), and saving the PDF it returns.
import { replaceState } from '$app/navigation';
import { ApiError } from './api';
import { getLocale, isLocale, setLocale } from './paraglide/runtime';

/**
 * Loads a print page's data once with the one-time token from the URL fragment, then takes the token out of the
 * address bar and history. Switches to the export's language (FR-I6): the cookie Gotenberg sends picks one, and if it
 * differs from the export's, the export's wins; the page renders after this, so no reload is needed.
 * Answers 'expired' for a used or expired token, 'failed' for any other error.
 */
export async function loadPrint<T extends { lang: string }>(get: (token: string) => Promise<T>): Promise<T | 'expired' | 'failed'> {
	try {
		const data = await get(location.hash.slice(1));
		if (data.lang !== getLocale() && isLocale(data.lang)) setLocale(data.lang, { reload: false });
		document.documentElement.lang = getLocale();
		return data;
	} catch (err) {
		return err instanceof ApiError && err.status === 404 ? 'expired' : 'failed';
	} finally {
		replaceState(location.pathname, {}); // after an await: the router has started
	}
}

/**
 * A failed print must not become a PDF. Call it once its message shows: an uncaught error fails Gotenberg's print
 * (failOnConsoleExceptions), then printReady ends its wait. This order was tested on the real Gotenberg: the error
 * first gives 409 at once; printReady first would print the message as the PDF.
 */
export function failPrint() {
	setTimeout(() => {
		throw new Error('print failed');
	});
	setTimeout(() => {
		window.printReady = true;
	});
}

/** Lays the page out now, so the fonts its text needs start loading, and waits for them. */
export async function fontsLoaded() {
	document.body.getBoundingClientRect();
	await document.fonts.ready;
}

/** Saves an exported file, such as a PDF, under its name. */
export function saveFile(name: string, blob: Blob) {
	const a = document.createElement('a');
	a.href = URL.createObjectURL(blob);
	a.download = name;
	a.click();
	// Not at once: some browsers read the blob after click() returns.
	setTimeout(() => URL.revokeObjectURL(a.href), 10_000);
}
