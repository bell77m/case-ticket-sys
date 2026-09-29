import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { axeViolations, guestTicket, signIn, ticketForEveryGraph } from './helpers';

// T3.05 (FR-P4, FR-I6): Export PDF prints the dashboard through the real Gotenberg (docker compose up) in the viewer's
// language. The PDFs stay in test-results/ for a person to look at.

const msg = (locale: string, key: string) => JSON.parse(readFileSync(`messages/${locale}.json`, 'utf8'))[key] as string;

/** Exports the dashboard in `locale`, saves the PDF as test-results/export-<locale>.pdf and returns its bytes as latin1 text. */
async function exportPdf(page: Page, locale: string) {
	await page.context().addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: 'http://localhost:5173' }]);
	await page.goto('/staff/reports');
	await expect(page.locator('canvas')).toHaveCount(7);

	const download = page.waitForEvent('download', { timeout: 60_000 });
	await page.getByRole('button', { name: msg(locale, 'reports_export'), exact: true }).click();
	// Busy while Gotenberg prints (a few seconds).
	await expect(page.getByRole('button', { name: msg(locale, 'reports_exporting'), exact: true })).toBeDisabled();
	const file = await download;
	expect(file.suggestedFilename()).toMatch(/^tickets-report-\d{4}-\d{2}-\d{2}\.pdf$/);
	const path = `test-results/export-${locale}.pdf`;
	await file.saveAs(path);
	await expect(page.getByRole('button', { name: msg(locale, 'reports_export'), exact: true })).toBeEnabled();

	const pdf = readFileSync(path);
	console.log(`${path}: ${pdf.length} bytes`);
	expect(pdf.subarray(0, 5).toString('latin1')).toBe('%PDF-');
	expect(pdf.length).toBeGreaterThan(50_000);
	const text = pdf.toString('latin1');
	// Chromium (Skia/PDF) prints each chart canvas as a color image with an alpha mask (/SMask); box shadows become
	// gray JPEGs without one, so /SMask counts the charts: 7 graphs, all with data.
	expect(text).toContain('/Subtype /Image');
	expect(text.match(/\/SMask \d+ 0 R/g)?.length ?? 0).toBe(7);
	return text;
}

test('Export PDF downloads the report in Thai and in Burmese, with charts and the self-hosted fonts', async ({ page, request }) => {
	test.slow(); // two Gotenberg prints
	await signIn(page, 'root');
	await expect(page).toHaveURL(/\/staff$/);
	await ticketForEveryGraph(page, request);

	expect(await exportPdf(page, 'th')).toContain('NotoSansThai');
	expect(await exportPdf(page, 'my')).toContain('NotoSansMyanmar');
});

// T3.06 (FR-L1, FR-I6): here, not in activity.spec.ts, so every Gotenberg print runs in this one worker. Gotenberg
// prints one page at a time; prints from parallel files queue past its 30-second limit.
test('Export PDF on the activity log downloads it in Thai with the Thai font', async ({ page, request }) => {
	test.slow(); // a Gotenberg print
	const { id } = await guestTicket(request, 'The handheld scanner battery does not charge.'); // a filter with one row
	await signIn(page, 'root');
	await expect(page).toHaveURL(/\/staff$/);
	await page.context().addCookies([{ name: 'PARAGLIDE_LOCALE', value: 'th', url: 'http://localhost:5173' }]);
	await page.goto(`/staff/admin/activity?ticket=${id}`);
	await expect(page.locator('ul.cards > li')).toHaveCount(1);

	const download = page.waitForEvent('download', { timeout: 60_000 });
	await page.getByRole('button', { name: msg('th', 'reports_export'), exact: true }).click();
	await expect(page.getByRole('button', { name: msg('th', 'reports_exporting'), exact: true })).toBeDisabled();
	const file = await download;
	expect(file.suggestedFilename()).toMatch(/^activity-log-\d{4}-\d{2}-\d{2}\.pdf$/);
	const path = 'test-results/activity-th.pdf';
	await file.saveAs(path);
	const pdf = readFileSync(path);
	console.log(`${path}: ${pdf.length} bytes`);
	expect(pdf.subarray(0, 5).toString('latin1')).toBe('%PDF-');
	expect(pdf.toString('latin1')).toContain('NotoSansThai'); // Thai month names in the times (FR-I6)
});

test('the print page without a valid token says the link expired, throws, then sets printReady', async ({ page }) => {
	const errors: string[] = [];
	page.on('pageerror', (e) => errors.push(e.message));
	await page.goto('/print/report#bogus');
	await expect(page.getByRole('alert')).toContainText(msg('en', 'print_expired'));
	await page.waitForFunction(() => window.printReady === true);
	// The uncaught error is what makes Gotenberg refuse the print (failOnConsoleExceptions).
	await expect.poll(() => errors).toEqual(['print failed']);
	expect(new URL(page.url()).hash).toBe(''); // the token leaves the address bar
	// No staff chrome: no language picker or account menu.
	await expect(page.getByRole('button', { name: /English/ })).toHaveCount(0);
});

test('Gotenberg refuses to print a failed print page, so no PDF comes out', async ({ request }) => {
	// What the API sends (export.go), for a token that does not exist. The API turns any non-200 into 502 pdf.failed.
	const gotenberg = process.env.GOTENBERG_URL || 'http://localhost:3000';
	const base = process.env.PRINT_BASE_URL || 'http://host.docker.internal:5173';
	const res = await request.post(`${gotenberg}/forms/chromium/convert/url`, {
		multipart: { url: `${base}/print/report#bogus`, waitForExpression: 'window.printReady === true', failOnConsoleExceptions: 'true' },
		timeout: 30_000
	});
	expect(res.status()).toBe(409);
});

test('the print page shows the export filters in its title block, then sets printReady without errors', async ({ page }) => {
	await signIn(page, 'root');
	await expect(page).toHaveURL(/\/staff$/);
	const report = await (await page.request.get('/api/staff/reports?lang=en')).json();
	const filters = { from: report.period.from, to: report.period.to, tz: 'UTC', building: 'Building A', category: null, category_id: null };
	await page.route('**/api/print/report', (route) => route.fulfill({ json: { ...report, filters, lang: 'en' } }));
	const errors: string[] = [];
	page.on('pageerror', (e) => errors.push(e.message));
	await page.goto('/print/report#token');
	await page.waitForFunction(() => window.printReady === true);
	await expect(page.locator('header')).toContainText('Building: Building A');
	await expect(page.locator('header')).toContainText('Category: All categories');
	await expect(page.locator('canvas').first()).toBeVisible();
	expect(errors).toEqual([]);
});

test('a failed export shows a translated error', async ({ page }) => {
	await signIn(page, 'root');
	await expect(page).toHaveURL(/\/staff$/);
	await page.route('**/api/staff/reports/export', (route) => route.fulfill({ status: 503, json: { error: 'pdf.unavailable' } }));
	await page.goto('/staff/reports');
	await page.getByRole('button', { name: 'Export PDF', exact: true }).click();
	await expect(page.getByRole('alert')).toContainText(msg('en', 'reports_err_pdf_unavailable'));
	await expect(page.getByRole('button', { name: 'Export PDF', exact: true })).toBeEnabled();
	expect(await axeViolations(page)).toEqual([]);
});

test('an Agent has no Export button and the API refuses it; a Viewer has it', async ({ page }) => {
	await signIn(page, 'agent');
	await expect(page).toHaveURL(/\/staff$/);
	await page.goto('/staff/reports');
	await expect(page.getByRole('alert')).toHaveText(/You do not have permission to open this page./);
	await expect(page.getByRole('button', { name: 'Export PDF' })).toHaveCount(0);
	const refused = await page.request.post('/api/staff/reports/export', { data: { lang: 'en' } });
	expect(refused.status()).toBe(403);

	await page.context().clearCookies();
	await signIn(page, 'viewer');
	await expect(page).toHaveURL(/\/staff$/);
	await page.goto('/staff/reports');
	await expect(page.getByRole('button', { name: 'Export PDF', exact: true })).toBeVisible();
	expect(await axeViolations(page)).toEqual([]);
});
