import { expect, test, type Page, type Response } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { baseURL, devPassword, guestTicket, signIn } from './helpers';

// T3.17: the whole ticket life in each language, through the UI only, under the script and style CSP (T2.14).
// The CSP comes from kit.csp: a response header from Vite, a <meta> tag in the built index.html the Go server serves.
// Vite adds 'unsafe-inline' to style-src in dev, so the style policy is only proven against the built app
// (make serve-built, then E2E_BASE_URL=http://localhost:8080).

const locales = ['en', 'zh-CN', 'my', 'th'];
const messages = Object.fromEntries(locales.map((l) => [l, JSON.parse(readFileSync(`messages/${l}.json`, 'utf8'))]));

const jpeg = Buffer.concat([Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0, 0x10]), Buffer.from('JFIF'), Buffer.alloc(2000, 1)]);
const mp4 = Buffer.concat([Buffer.from([0, 0, 0, 0x18]), Buffer.from('ftypisom'), Buffer.alloc(3000, 2)]);

/** Collects the browser's CSP refusals ("Refused to execute inline script ...") into errors. */
function cspErrors(page: Page, errors: string[] = []) {
	page.on('console', (msg) => {
		if (msg.type() === 'error' && msg.text().includes('Content Security Policy')) errors.push(msg.text());
	});
	return errors;
}

/** The page's CSP: the response header plus the <meta> tag of the built app. */
async function policy(page: Page, res: Response | null) {
	// evaluate, not a locator: the tag is in the served HTML or not at all, so there is nothing to wait for.
	const meta = await page.evaluate(() => document.querySelector('meta[http-equiv="content-security-policy" i]')?.getAttribute('content'));
	return `${res?.headers()['content-security-policy'] ?? ''}; ${meta ?? ''}`;
}

for (const locale of locales) {
	test(`smoke (${locale}): guest reports with a photo, staff resolves, dashboard follows, guest confirms`, async ({ page, browser }) => {
		test.setTimeout(90_000);
		const t = (key: string) => messages[locale][key] as string;
		const esc = (s: string) => s.replace(/[.*+?^$()|[\]\\]/g, '\\$&');
		const errors = cspErrors(page);

		// Guest: the form on a phone, with a photo.
		await page.context().addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: baseURL }]);
		const form = await page.goto('/report');
		expect(await policy(page, form)).toMatch(/script-src 'self'/);
		await page.getByLabel(t('form_name')).fill('Smoke Guest');
		await page.getByLabel(t('form_employee_id')).fill('E7007');
		await page.getByLabel(t('form_building')).selectOption({ index: 1 });
		await page.getByLabel(t('form_floor')).selectOption({ index: 1 });
		await page.getByLabel(t('form_line')).selectOption({ index: 1 });
		await page.getByLabel(t('form_details')).fill(`Smoke test ${locale} ${Date.now()}: the label printer is jammed.`);
		// A photo and a video: previews (blob:) on the form, and both evidence kinds (blob:) on the tracking page.
		await page.getByLabel(t('form_evidence')).setInputFiles([
			{ name: 'printer.jpg', mimeType: 'image/jpeg', buffer: jpeg },
			{ name: 'printer.mp4', mimeType: 'video/mp4', buffer: mp4 }
		]);
		await page.getByRole('button', { name: t('form_submit') }).click();
		const created = page.getByRole('heading', { name: new RegExp(esc(t('form_created')).replace('{id}', '(\\d+)')) });
		await expect(created).toBeVisible();
		await expect(page.getByText(t('form_files_attached').replace('{count}', '2'))).toBeVisible();
		const id = (await created.textContent())!.match(/#(\d+)/)![1];
		const trackURL = await page.getByLabel(t('track_link_label')).inputValue();

		// A category of its own: the dashboard, filtered to it, counts only this ticket while other specs change theirs.
		const root = await browser.newContext();
		expect((await root.request.post('/api/auth/login', { data: { username: 'root', password: devPassword } })).ok()).toBe(true);
		const category = `Smoke ${locale} ${Date.now()}`;
		const made = await root.request.post('/api/staff/categories', {
			data: { name: { en: category, 'zh-CN': category, my: category, th: category } }
		});
		expect(made.ok()).toBe(true);
		const categoryId = (await made.json()).id;

		// An Agent signs in on /login in the same language; a Viewer (report.view) watches the dashboard in another browser.
		const agent = await browser.newContext();
		const viewer = await browser.newContext();
		for (const c of [agent, viewer]) await c.addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: baseURL }]);
		const s = await agent.newPage();
		cspErrors(s, errors);
		await s.goto('/login');
		await s.getByLabel(t('login_username')).fill('agent');
		await s.getByLabel(t('login_password')).fill(devPassword);
		await s.getByRole('button', { name: t('login_button') }).click();
		await expect(s).toHaveURL(/\/staff$/);

		expect((await viewer.request.post('/api/auth/login', { data: { username: 'viewer', password: devPassword } })).ok()).toBe(true);
		const d = await viewer.newPage();
		cspErrors(d, errors);
		const stream = d.waitForResponse((r) => r.url().endsWith('/api/events'));
		await d.goto(`/staff/reports?category_id=${categoryId}`);
		expect((await stream).status()).toBe(200);
		const resolved = d.locator('.tile', { has: d.getByText(t('reports_card_resolved'), { exact: true }) }).locator('.tile-value');
		await expect(resolved).toHaveText(/^[0၀]$/); // Intl digits: Burmese has its own

		// Assign to me, start work, resolve. After each action the page reloads the ticket and refills the form, so each
		// step waits for the page to show the last one landed; a choice made earlier is overwritten and Save sends nothing.
		await s.goto(`/staff/tickets/${id}`);
		const header = s.locator('header', { has: s.getByRole('heading', { level: 1 }) });
		const assign = s.getByRole('button', { name: t('detail_assign_me') });
		await assign.click();
		await expect(assign).toBeHidden();
		for (const status of ['in_progress', 'resolved']) {
			await s.getByLabel(t('queue_col_status'), { exact: true }).selectOption(status);
			if (status === 'in_progress') await s.getByLabel(t('detail_f_category')).selectOption({ label: category });
			const answer = s.waitForResponse((r) => r.url().includes(`/api/staff/tickets/${id}`) && r.request().method() === 'PATCH');
			await s.getByRole('button', { name: t('detail_save') }).click();
			expect((await answer).ok()).toBe(true);
			await expect(header).toContainText(t(`status_${status}`));
		}

		// The dashboard counts it without a page reload (T3.04).
		await expect(resolved).toHaveText(/^[1၁]$/, { timeout: 10_000 });

		// Guest: opens both files of evidence (fetched with the token, shown from blob: URLs), then confirms the fix on the
		// tracking link; the ticket closes.
		await page.goto(trackURL);
		const views = page.locator('ul.files').getByRole('button');
		for (const left of [1, 0]) {
			await views.first().click();
			await expect(views).toHaveCount(left);
		}
		await expect(page.locator('ul.files img[src^="blob:"], ul.files video[src^="blob:"]')).toHaveCount(2);
		await page.getByRole('button', { name: t('track_confirm') }).click();
		await expect(page.getByText(t('track_closed'))).toBeVisible();

		expect((await root.request.patch(`/api/staff/categories/${categoryId}`, { data: { is_active: false } })).ok()).toBe(true);
		await Promise.all([agent.close(), viewer.close(), root.close()]);
		expect(errors).toEqual([]);
	});
}

// T2.14, T3.17: the CSP sets default-src, script-src and style-src, and no page the smoke flow skips is refused anything.
test('every page loads under the CSP', async ({ page, request }) => {
	const errors = cspErrors(page);
	const res = await page.goto('/');
	const csp = await policy(page, res);
	// default-src covers what script-src and style-src do not (fetch, images, media, fonts, frames): 'self' only.
	for (const directive of ["default-src 'self'", "script-src 'self'", "style-src 'self'"]) expect(csp).toContain(directive);
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

	const { id, token } = await guestTicket(request, `CSP check ${Date.now()}: the monitor is blank.`);
	await signIn(page, 'root');
	for (const path of ['/staff', '/staff/admin/staff', '/staff/admin/roles', '/staff/admin/lookups', '/staff/admin/activity', `/staff/tickets/${id}`, `/track#${token}`]) {
		await page.goto(path);
		await expect(page.getByRole('heading', { level: 1 }).first()).toBeVisible();
	}
	expect(errors).toEqual([]);
});
