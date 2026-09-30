import { expect, test } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { baseURL } from './helpers';

const axeSource = readFileSync('node_modules/axe-core/axe.min.js', 'utf8');

// DESIGN.md "Accessibility": the welcome page and guest form have no WCAG 2.2 AA violations, in English and Burmese.
for (const [name, path] of [
	['welcome', '/'],
	['guest-form', '/report']
]) {
	for (const locale of ['en', 'my']) {
		test(`${name} accessibility (${locale})`, async ({ page, context }) => {
			await context.addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: baseURL }]);
			await page.goto(path);
			await page.getByRole('heading', { level: 1 }).waitFor();
			await page.evaluate(axeSource); // through DevTools: the CSP (T3.17) refuses an injected inline script
			const violations = await page.evaluate(async () => {
				// @ts-expect-error axe is injected above
				const r = await window.axe.run(document, { runOnly: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'] });
				return r.violations.map((v: { id: string; nodes: unknown[] }) => `${v.id} (${v.nodes.length})`);
			});
			expect(violations).toEqual([]);
			await page.screenshot({ path: `test-results/${name}-${locale}.png`, fullPage: true });
		});
	}
}
