import { expect, test, type APIRequestContext, type Browser } from '@playwright/test';
import { devPassword, guestTicket } from './helpers';

// T3.04 (FR-P3): a change made elsewhere shows on open staff pages within 2 seconds, without a reload.
// Other spec files open tickets at the same time, so checks are relative and never assume exact counts.

async function signInAPI(request: APIRequestContext) {
	const res = await request.post('/api/auth/login', { data: { username: 'root', password: devPassword } });
	expect(res.ok()).toBe(true);
}

/** A separate browser signed in as root, on url, once its event stream is open (so no change is missed). */
async function watch(browser: Browser, url: string) {
	const context = await browser.newContext();
	await signInAPI(context.request);
	const page = await context.newPage();
	const stream = page.waitForResponse((r) => r.url().endsWith('/api/events'));
	await page.goto(url);
	expect((await stream).status()).toBe(200);
	return page;
}

/** Waits for check and logs how long it took since `from`: the 2 s budget runs from the change's response. */
async function within2s(what: string, from: number, check: () => Promise<void>) {
	await check();
	console.log(`${what}: ${Date.now() - from} ms`);
}

test('the queue and ticket detail follow changes made in another browser', async ({ browser, request }) => {
	const queue = await watch(browser, '/staff');
	const { id } = await guestTicket(request, `Live update check ${Date.now()}: the label printer is offline.`);
	let t = Date.now();
	const card = queue.getByRole('listitem').filter({ has: queue.locator(`a[href="/staff/tickets/${id}"]`) });
	await within2s('queue shows new ticket', t, () => expect(card).toBeVisible({ timeout: 2000 }));
	await expect(card).toContainText('New');

	const detail = await watch(browser, `/staff/tickets/${id}`);
	const header = detail.locator('header', { has: detail.getByRole('heading', { level: 1 }) });
	await expect(header).toContainText('New');

	// Another staff member (context A) starts work on it.
	const a = await browser.newContext();
	await signInAPI(a.request);
	expect((await a.request.patch(`/api/staff/tickets/${id}`, { data: { status: 'in_progress' } })).ok()).toBe(true);
	t = Date.now();
	await within2s('queue badge', t, () => expect(card).toContainText('In Progress', { timeout: 2000 }));
	await within2s('detail badge', t, () => expect(header).toContainText('In Progress', { timeout: 2000 }));

	// Unsaved edits in the Details form are kept while the rest of the page follows; a notice offers to refill the form.
	await detail.getByLabel('Priority').selectOption('high');
	expect((await a.request.patch(`/api/staff/tickets/${id}`, { data: { status: 'resolved' } })).ok()).toBe(true);
	t = Date.now();
	const notice = detail.getByText('Someone else changed this ticket');
	await within2s('detail notice', t, () => expect(notice).toBeVisible({ timeout: 2000 }));
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
	const t = Date.now();
	await within2s('reports New card', t, () => expect.poll(count, { timeout: 2000 }).toBeGreaterThan(before));
});
