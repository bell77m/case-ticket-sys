import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { axeBothSizes, guestTicket, signIn } from './helpers';

// T3.03 (FR-P1, FR-P2, FR-I5): the reports dashboard. Only report.view opens it; the API refuses the rest (FR-R3).

const cards = [
	'Open tickets',
	'Unassigned',
	'Urgent open',
	'New',
	'Resolved',
	'First response (median)',
	'Resolution time (median)'
];
const graphs = [
	'Opened and resolved per day',
	'Open tickets by status',
	'Open tickets by priority',
	'Tickets by category',
	'Tickets by location',
	'Staff workload by priority',
	'Weekly median first response and resolution'
];

const graphCard = (page: Page, title: string) =>
	page.locator('section').filter({ has: page.getByRole('heading', { level: 2, name: title, exact: true }) });

const sideways = (page: Page) =>
	page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);

/** Gives every graph data today: an urgent ticket assigned to the signed-in staff member, with a public reply. */
async function ticketForEveryGraph(page: Page, request: APIRequestContext) {
	const { id } = await guestTicket(request, 'The barcode scanner at the packing line stopped reading labels.');
	const me = await (await page.request.get('/api/auth/me')).json();
	expect((await page.request.patch(`/api/staff/tickets/${id}`, { data: { priority: 'urgent' } })).ok()).toBe(true);
	expect((await page.request.put(`/api/staff/tickets/${id}/assignee`, { data: { assignee_id: me.id } })).ok()).toBe(true);
	const reply = await page.request.post(`/api/staff/tickets/${id}/comments`, { data: { body: 'Looking into it.', internal: false } });
	expect(reply.ok()).toBe(true);
}

test('root sees the cards, the summary and 7 graphs, each with a table view', async ({ page, request }) => {
	test.slow(); // seven toggles, two viewports and two axe runs
	await signIn(page, 'root');
	await expect(page).toHaveURL(/\/staff$/);
	await ticketForEveryGraph(page, request);

	await page.getByRole('button', { name: 'Dev Root Admin' }).click();
	await page.getByRole('link', { name: 'Reports' }).click();
	await expect(page.getByRole('heading', { level: 1 })).toHaveText('Reports');
	await expect(page.locator('#account-menu')).toBeHidden();

	await expect(page.locator('.tile-label')).toHaveText(cards);
	await expect(page.getByRole('heading', { name: 'Summary' })).toBeVisible();
	await expect(page.locator('section').filter({ hasText: 'Summary' }).getByRole('listitem').first()).toBeVisible();

	for (const title of graphs) {
		const card = graphCard(page, title);
		const canvas = card.locator('canvas');
		await expect(canvas, title).toBeVisible();
		await expect(canvas).toHaveAttribute('role', 'img');
		await expect(canvas).toHaveAttribute('aria-label', title);

		// "Show as table" swaps the canvas for a table with the same values, and back.
		const toggle = card.getByRole('button', { name: 'Show as table' });
		await expect(toggle).toHaveAttribute('aria-pressed', 'false');
		await toggle.click();
		await expect(toggle).toHaveAttribute('aria-pressed', 'true');
		const table = card.getByRole('table', { name: title });
		await expect(table).toBeVisible();
		await expect(table.locator('thead th').first()).toBeVisible();
		expect(await table.locator('tbody tr').count(), title).toBeGreaterThan(0);
		await expect(canvas).toHaveCount(0);
		await toggle.click();
		await expect(card.locator('canvas')).toBeVisible();
	}

	// Location drills down from buildings to floors, and back.
	const location = graphCard(page, 'Tickets by location');
	await location.getByRole('button', { name: 'Show as table' }).click();
	const firstBuilding = location.locator('tbody th button').first();
	const buildingName = (await firstBuilding.textContent())!.trim();
	await firstBuilding.click();
	await expect(location.locator('thead th').first()).toHaveText('Floor');
	await expect(location.getByRole('button', { name: 'Back to all buildings' })).toBeFocused();
	await expect(location).toContainText(buildingName);
	await location.getByRole('button', { name: 'Back to all buildings' }).click();
	await expect(location.locator('thead th').first()).toHaveText('Building');
	await location.getByRole('button', { name: 'Show as table' }).click();

	// One column on phones, two from 1024px.
	const first = graphCard(page, graphs[0]);
	const second = graphCard(page, graphs[1]);
	let a = (await first.boundingBox())!;
	let b = (await second.boundingBox())!;
	expect(b.y).toBeGreaterThanOrEqual(a.y + a.height);
	expect(Math.round(b.x)).toBe(Math.round(a.x));
	await page.setViewportSize({ width: 1280, height: 800 });
	await expect(async () => {
		a = (await first.boundingBox())!;
		b = (await second.boundingBox())!;
		expect(Math.round(b.y)).toBe(Math.round(a.y));
		expect(b.x).toBeGreaterThanOrEqual(a.x + a.width);
	}).toPass();
	await page.setViewportSize({ width: 390, height: 844 });

	await axeBothSizes(page, 'reports');
	await graphCard(page, 'Staff workload by priority').getByRole('button', { name: 'Show as table' }).click();
	await axeBothSizes(page, 'reports-table');

	// WCAG 1.4.10: no sideways scroll at 320px, with charts and with the widest table open.
	await page.setViewportSize({ width: 320, height: 700 });
	// Chart.js resizes on a ResizeObserver callback, a frame after the viewport changes.
	await expect.poll(() => sideways(page), { message: 'with a table' }).toBe(0);
	await graphCard(page, 'Staff workload by priority').getByRole('button', { name: 'Show as table' }).click();
	await expect(graphCard(page, 'Staff workload by priority').locator('canvas')).toBeVisible();
	await expect.poll(() => sideways(page), { message: 'with charts' }).toBe(0);
});

test('a new date range goes to the URL and reloads the data', async ({ page }) => {
	await signIn(page, 'root');
	await expect(page).toHaveURL(/\/staff$/);
	await page.goto('/staff/reports');
	const to = page.getByLabel('To', { exact: true });
	await expect(to).not.toHaveValue('');
	const end = new Date(`${await to.inputValue()}T00:00:00Z`);
	const start = new Date(end.getTime() - 6 * 86_400_000).toISOString().slice(0, 10);

	const reload = page.waitForResponse((r) => r.url().includes('/api/staff/reports') && r.url().includes(`from=${start}`));
	await page.getByLabel('From', { exact: true }).fill(start);
	expect((await reload).status()).toBe(200);
	await expect(page).toHaveURL(new RegExp(`from=${start}`));

	// Seven days, seven rows.
	const daily = graphCard(page, 'Opened and resolved per day');
	await daily.getByRole('button', { name: 'Show as table' }).click();
	await expect(daily.locator('tbody tr')).toHaveCount(7);

	// Selects only apply through the Apply button (frontend.md), then the filter is in the URL.
	await page.getByLabel('Category', { exact: true }).selectOption({ index: 1 });
	await expect(page).not.toHaveURL(/category_id=/);
	await page.getByRole('button', { name: 'Apply' }).click();
	await expect(page).toHaveURL(/category_id=\d+/);
	await expect(page).toHaveURL(new RegExp(`from=${start}`));
});

test('the Open card opens the queue with the open status filter', async ({ page }) => {
	await signIn(page, 'root');
	await expect(page).toHaveURL(/\/staff$/);
	await page.goto('/staff/reports');
	await page.getByRole('link', { name: /Open tickets/ }).click();
	await expect(page).toHaveURL(/\/staff\?status=open$/);
	await expect(page.getByRole('heading', { level: 1 })).toHaveText('Tickets');
	await expect(page.getByLabel('Status')).toHaveValue('open');
});

test('an Agent has no reports; a Viewer does', async ({ page }) => {
	await signIn(page, 'agent');
	await expect(page).toHaveURL(/\/staff$/);
	await page.getByRole('button', { name: 'Dev Agent' }).click();
	await expect(page.getByRole('link', { name: 'Change password' })).toBeVisible();
	await expect(page.getByRole('link', { name: 'Reports' })).toHaveCount(0);
	await page.goto('/staff/reports');
	await expect(page.getByRole('alert')).toHaveText(/You do not have permission to open this page./);
	await expect(page.locator('canvas')).toHaveCount(0);
	expect((await page.request.get('/api/staff/reports')).status()).toBe(403);

	await page.context().clearCookies();
	await signIn(page, 'viewer');
	await expect(page).toHaveURL(/\/staff$/);
	await page.getByRole('button', { name: 'Dev Viewer' }).click();
	await page.getByRole('link', { name: 'Reports' }).click();
	await expect(page.getByRole('heading', { level: 1 })).toHaveText('Reports');
	await expect(page.locator('.tile-label')).toHaveText(cards);
	await expect(page.locator('canvas').first()).toBeVisible();
});
