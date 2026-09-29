import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { axeBothSizes, devPassword, guestTicket, signIn } from './helpers';

// T3.06 (FR-L1, FR-I6, FR-P4): the activity log needs audit.view. Done when an Agent gets 403 and a Team Lead sees
// the log. No Team Lead is seeded, so root adds one through the API for this file and deactivates it at the end.

const msg = (locale: string, key: string) => JSON.parse(readFileSync(`messages/${locale}.json`, 'utf8'))[key] as string;
const baseURL = 'http://localhost:5173';
const stamp = Date.now();
const lead = { id: 0, username: `e2e-lead-${stamp}`, name: `E2E Lead ${stamp}`, password: 'e2e-own-password-1' };
let root: APIRequestContext;

test.beforeAll(async ({ playwright }) => {
	root = await playwright.request.newContext({ baseURL });
	expect((await root.post('/api/auth/login', { data: { username: 'root', password: devPassword } })).ok()).toBe(true);
	const roles: { id: number; name: string }[] = await (await root.get('/api/staff/roles')).json();
	const temp = 'e2e-temp-password';
	const created = await root.post('/api/staff/accounts', {
		data: { name: lead.name, username: lead.username, role_id: roles.find((r) => r.name === 'Team Lead')!.id, password: temp }
	});
	expect(created.ok()).toBe(true);
	lead.id = (await created.json()).id;
	// The first sign-in must replace the temporary password (FR-A8).
	const own = await playwright.request.newContext({ baseURL });
	expect((await own.post('/api/auth/login', { data: { username: lead.username, password: temp } })).ok()).toBe(true);
	const changed = await own.post('/api/auth/password', { data: { current_password: temp, new_password: lead.password } });
	expect(changed.ok()).toBe(true);
	await own.dispose();
});

// Accounts are never deleted; their audit rows stay.
test.afterAll(async () => {
	if (lead.id) expect((await root.patch(`/api/staff/accounts/${lead.id}`, { data: { is_active: false } })).ok()).toBe(true);
	await root.dispose();
});

/** The log's rows as phone-size cards. */
const rows = (page: Page) => page.locator('ul.cards > li');

async function openAsLead(page: Page) {
	await signIn(page, lead.username, lead.password);
	await expect(page).toHaveURL(/\/staff$/);
	await page.goto('/staff/admin/activity');
	await expect(page.getByRole('heading', { level: 1 })).toHaveText('Activity log');
}

test('an Agent has no Activity log link, the page says no permission and the API refuses', async ({ page }) => {
	await signIn(page, 'agent');
	await expect(page).toHaveURL(/\/staff$/);
	await page.getByRole('button', { name: 'Dev Agent' }).click();
	await expect(page.locator('#account-menu')).toContainText('agent');
	await expect(page.getByRole('link', { name: 'Activity log' })).toHaveCount(0);

	await page.goto('/staff/admin/activity');
	await expect(page.getByRole('alert')).toContainText('You do not have permission to open this page.');
	await expect(rows(page)).toHaveCount(0);
	// Hidden links are not the control (FR-R3).
	expect((await page.request.get('/api/staff/activity')).status()).toBe(403);
	expect((await page.request.post('/api/staff/activity/export', { data: { lang: 'en' } })).status()).toBe(403);
});

test('a Team Lead opens the log from the account menu and sees translated rows', async ({ page }) => {
	await signIn(page, lead.username, lead.password);
	await expect(page).toHaveURL(/\/staff$/);
	await page.getByRole('button', { name: lead.name }).click();
	await page.getByRole('link', { name: 'Activity log' }).click();
	await expect(page.getByRole('heading', { level: 1 })).toHaveText('Activity log');
	await expect(page.locator('#account-menu')).toBeHidden();
	await expect(rows(page).filter({ hasText: 'Signed in' }).first()).toBeVisible();
	await expect(page.getByText('login.success')).toHaveCount(0);

	// Staff filter: the Team Lead's own rows, oldest last: API sign-in, password change, this sign-in.
	await page.getByLabel('Staff', { exact: true }).selectOption({ label: lead.name });
	await page.getByRole('button', { name: 'Apply' }).click();
	await expect(page).toHaveURL(new RegExp(`staff=${lead.id}`));
	await expect(rows(page)).toHaveCount(3);
	await expect(rows(page).filter({ hasNotText: lead.name })).toHaveCount(0);
	await expect(rows(page).nth(0)).toContainText('Signed in');
	await expect(rows(page).nth(1)).toContainText('Password changed');
	await expect(rows(page).nth(1)).toContainText(lead.username); // target
	await axeBothSizes(page, 'activity');
});

test('filters by action and by ticket, keep them in the URL, and page through the log', async ({ page, request }) => {
	const { id } = await guestTicket(request, 'The label printer on line 2 prints blank labels.');
	await openAsLead(page);
	expect((await page.request.patch(`/api/staff/tickets/${id}`, { data: { priority: 'high' } })).ok()).toBe(true);

	// Pages: newest first, 50 per page. The dev DB holds far more than 50 rows.
	await expect(page.getByText(/^Page 1 of \d+$/)).toBeVisible();
	await page.getByRole('button', { name: 'Next' }).click();
	await expect(page).toHaveURL(/page=2/);
	await expect(page.getByText(/^Page 2 of \d+$/)).toBeVisible();

	await page.getByLabel('Action', { exact: true }).selectOption({ label: 'Signed in' });
	await page.getByRole('button', { name: 'Apply' }).click();
	await expect(page).toHaveURL(/action=login\.success/);
	await expect(page).not.toHaveURL(/page=/); // a new filter starts at page 1
	await expect(rows(page).first()).toBeVisible();
	await expect(rows(page).filter({ hasNotText: 'Signed in' })).toHaveCount(0);

	await page.getByLabel('Action', { exact: true }).selectOption({ label: 'All actions' });
	await page.getByLabel('Ticket number').fill(`#${id}`);
	await page.getByRole('button', { name: 'Apply' }).click();
	await expect(page).toHaveURL(new RegExp(`ticket=${id}`));
	await expect(page).not.toHaveURL(/action=/);
	await expect(rows(page)).toHaveCount(2);
	await expect(rows(page).nth(0)).toContainText('Priority changed');
	await expect(rows(page).nth(0)).toContainText('Not set → High');
	await expect(rows(page).nth(1)).toContainText('Ticket created');
	await expect(rows(page).nth(1)).toContainText('Guest');
	await expect(rows(page).getByRole('link', { name: `#${id}` })).toHaveCount(2);

	// A bad value shows under its field.
	await page.getByLabel('Ticket number').fill('abc');
	await page.getByRole('button', { name: 'Apply' }).click();
	await expect(page.getByText('Enter a ticket number, such as 12.')).toBeVisible();
	await expect(page.getByLabel('Ticket number')).toHaveAttribute('aria-invalid', 'true');
});

// Export PDF against the real Gotenberg is in export.spec.ts: one worker runs every print there, since Gotenberg
// prints one page at a time and parallel prints from several files queue past its 30-second limit.

test('the print page shows the filters and the truncation line, then sets printReady', async ({ page }) => {
	await openAsLead(page);
	const log = await (await page.request.get('/api/staff/activity?lang=en&page_size=20')).json();
	const filters = { staff: { id: lead.id, name: lead.name }, action: ['login.success', 'staff.password_changed'], ticket: 42, from: '2026-09-01', to: null, tz: 'Asia/Bangkok' };
	await page.route('**/api/print/activity', (route) => route.fulfill({ json: { items: log.items, truncated: true, filters, lang: 'en' } }));
	const errors: string[] = [];
	page.on('pageerror', (e) => errors.push(e.message));
	await page.goto('/print/activity#token');
	await page.waitForFunction(() => window.printReady === true);
	const header = page.locator('header');
	await expect(header).toContainText('Period: From Sep 1, 2026');
	await expect(header).toContainText(`Staff: ${lead.name}`);
	await expect(header).toContainText('Action: Signed in, Password changed');
	await expect(header).toContainText('Ticket: #42');
	await expect(header).toContainText('Times in Asia/Bangkok');
	await expect(header).toContainText('Showing the newest 20 entries.');
	await expect(page.getByRole('row')).toHaveCount(21); // header row + 20, at phone width too
	await page.screenshot({ path: 'test-results/activity-print.png', fullPage: true });
	expect(errors).toEqual([]);
});

test('the print page without a valid token says the link expired, throws, then sets printReady', async ({ page }) => {
	const errors: string[] = [];
	page.on('pageerror', (e) => errors.push(e.message));
	await page.goto('/print/activity#bogus');
	await expect(page.getByRole('alert')).toContainText(msg('en', 'activity_print_expired'));
	await page.waitForFunction(() => window.printReady === true);
	// The uncaught error is what makes Gotenberg refuse the print (failOnConsoleExceptions).
	await expect.poll(() => errors).toEqual(['print failed']);
	expect(new URL(page.url()).hash).toBe('');
});
