import { expect, test } from '@playwright/test';
import { baseURL, guestTicket, signIn } from './helpers';

// FR-T2, FR-I1: the queue summary stays readable and the page never scrolls sideways, in every language.
// At 768px seven table columns squeezed the summary to one word per line and overflowed in Burmese.
test('queue summary is readable at tablet widths in all languages', async ({ page, context, request }) => {
	test.slow(); // eight full page loads; with 6 workers they take about 30s
	await guestTicket(request, 'The label printer on line 1 prints blank labels since Monday morning.');
	await signIn(page, 'agent');
	await page.waitForURL('**/staff');
	for (const locale of ['en', 'zh-CN', 'my', 'th']) {
		await context.addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: baseURL }]);
		for (const width of [768, 1024]) {
			await page.setViewportSize({ width, height: 900 });
			await page.goto('/staff');
			const summary = page.locator('a[href^="/staff/tickets/"]:visible').first();
			await summary.waitFor();
			const where = `${locale} ${width}px`;
			// The table cell or card holding the link; the link's own box is only its longest wrapped line.
			const cell = await summary.locator('xpath=..').boundingBox();
			expect(cell!.width, `summary width, ${where}`).toBeGreaterThanOrEqual(200);
			const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
			expect(overflow, `sideways scroll, ${where}`).toBe(0);
		}
	}
});

// WCAG 1.4.10 Reflow: no sideways scroll at 320px. The language and account row must fit there.
test('no sideways scroll at 320px in all languages', async ({ page, context }) => {
	test.slow(); // twelve full page loads; with 6 workers they take about 30s
	await signIn(page, 'agent');
	await page.waitForURL('**/staff');
	await page.setViewportSize({ width: 320, height: 700 });
	for (const locale of ['en', 'zh-CN', 'my', 'th']) {
		await context.addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: baseURL }]);
		for (const path of ['/', '/report', '/staff']) {
			await page.goto(path);
			await page.getByRole('heading', { level: 1 }).waitFor();
			const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
			expect(overflow, `sideways scroll, ${path} ${locale}`).toBe(0);
		}
	}
});

// The icon link must be in the static HTML; added only after hydration, the browser first asks for /favicon.ico and gets 404.
test('first load has no console errors', async ({ page }) => {
	const errors: string[] = [];
	page.on('console', (m) => m.type() === 'error' && errors.push(`${m.text()} ${m.location().url}`));
	await page.goto('/');
	await page.getByRole('heading', { level: 1 }).waitFor();
	await page.waitForLoadState('networkidle');
	expect(errors).toEqual([]);
});
