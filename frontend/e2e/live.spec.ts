import { expect, test, type APIRequestContext, type Browser, type BrowserContext, type Locator } from '@playwright/test';
import { devPassword, guestTicket } from './helpers';

// T3.04 (FR-P3): a change made elsewhere shows on open staff pages within 2 seconds, without a reload.
// Other spec files open tickets at the same time, so checks are relative and never assume exact counts.

async function signInAPI(request: APIRequestContext) {
	const res = await request.post('/api/auth/login', { data: { username: 'root', password: devPassword } });
	expect(res.ok()).toBe(true);
}

const contexts: BrowserContext[] = [];
test.afterEach(() => Promise.all(contexts.splice(0).map((c) => c.close())));

/** A separate browser signed in as root. */
async function staffBrowser(browser: Browser) {
	const context = await browser.newContext();
	contexts.push(context);
	await signInAPI(context.request);
	return context;
}

/** A separate browser signed in as root, on url, once its event stream is open (so no change is missed). */
async function watch(browser: Browser, url: string) {
	const page = await (await staffBrowser(browser)).newPage();
	const stream = page.waitForResponse((r) => r.url().endsWith('/api/events'));
	await page.goto(url);
	expect((await stream).status()).toBe(200);
	return page;
}

/**
 * Polls probe every 50 ms until it returns true, at most 2 s after `from` (the change's response), and logs the delay.
 * Finer than a locator assertion, whose retries end up 1 s apart.
 */
async function within2s(what: string, from: number, probe: () => Promise<boolean | undefined>) {
	await expect.poll(probe, { message: what, intervals: [50], timeout: 2000 - (Date.now() - from) }).toBe(true);
	console.log(`${what}: ${Date.now() - from} ms`);
}
const has = (l: Locator, text: string) => async () => (await l.textContent())?.includes(text);

test('the queue and ticket detail follow changes made in another browser', async ({ browser, request }) => {
	const queue = await watch(browser, '/staff');
	const { id } = await guestTicket(request, `Live update check ${Date.now()}: the label printer is offline.`);
	let t = Date.now();
	const card = queue.getByRole('listitem').filter({ has: queue.locator(`a[href="/staff/tickets/${id}"]`) });
	await within2s('queue shows new ticket', t, () => card.isVisible());
	await expect(card).toContainText('New');

	const detail = await watch(browser, `/staff/tickets/${id}`);
	const header = detail.locator('header', { has: detail.getByRole('heading', { level: 1 }) });
	await expect(header).toContainText('New');

	// Another staff member (context A) starts work on it.
	const a = await staffBrowser(browser);
	expect((await a.request.patch(`/api/staff/tickets/${id}`, { data: { status: 'in_progress' } })).ok()).toBe(true);
	t = Date.now();
	await within2s('queue badge', t, has(card, 'In Progress'));
	await within2s('detail badge', t, has(header, 'In Progress'));

	// Unsaved edits in the Details form are kept while the rest of the page follows; a notice offers to refill the form.
	// The ticket ends resolved: a "Waiting on User" badge left in the open queue squeezes layout.spec's summary column.
	await detail.getByLabel('Priority').selectOption('high');
	expect((await a.request.patch(`/api/staff/tickets/${id}`, { data: { status: 'resolved' } })).ok()).toBe(true);
	t = Date.now();
	const notice = detail.getByText('Someone else changed this ticket');
	await within2s('detail notice', t, () => notice.isVisible());
	await expect(header).toContainText('Resolved');
	await expect(detail.getByLabel('Priority')).toHaveValue('high');
	await expect(detail.getByLabel('Status', { exact: true })).toHaveValue('in_progress');
	await detail.getByRole('button', { name: 'Reload form' }).click();
	await expect(notice).toBeHidden();
	await expect(detail.getByLabel('Priority')).toHaveValue('');
	await expect(detail.getByLabel('Status', { exact: true })).toHaveValue('resolved');
});

test('the reports dashboard counts a new ticket without a reload', async ({ browser, request }) => {
	const reports = await watch(browser, '/staff/reports');
	const value = reports.locator('.tile', { has: reports.getByText('New', { exact: true }) }).locator('.tile-value');
	const count = async () => Number((await value.textContent())?.replace(/\D/g, ''));
	await expect(value).toHaveText(/^[\d,]+$/);
	const before = await count();

	await guestTicket(request, `Live report check ${Date.now()}: the shared drive is read-only.`);
	await within2s('reports New card', Date.now(), async () => (await count()) > before);
});

test('after the event stream drops, the queue catches up on changes it missed', async ({ browser, request }) => {
	// The stream is down from the start (as after a pod restart); EventSource keeps retrying.
	const page = await (await staffBrowser(browser)).newPage();
	await page.route('**/api/events', (route) => route.abort());
	const loaded = page.waitForResponse((r) => r.url().includes('/api/staff/tickets?'));
	await page.goto('/staff');
	await loaded; // the queue has its data before the ticket exists
	const { id } = await guestTicket(request, `Missed event check ${Date.now()}: the scanner is offline.`);
	const card = page.getByRole('listitem').filter({ has: page.locator(`a[href="/staff/tickets/${id}"]`) });
	await expect(card).toBeHidden();
	// Back: the reopened stream sends no event for this ticket, so only the catch-up refresh can show it.
	await page.unroute('**/api/events');
	await expect(card).toBeVisible({ timeout: 10_000 });
});
