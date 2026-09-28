import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { axeViolations } from './helpers';

// T3.02 written summary (FR-P1, FR-I5, FR-I6) on the fixed sample report in /dev/components (no API or data needed).

const title = (locale: string) => JSON.parse(readFileSync(`messages/${locale}.json`, 'utf8')).summary_title as string;

/** Opens the gallery in `locale`; returns the summary sentences and the sample's values as that locale formats them. */
async function openSummary(page: Page, locale: string) {
	await page.context().addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: 'http://localhost:5173' }]);
	await page.goto('/dev/components');
	const card = page.locator('section').filter({ has: page.getByRole('heading', { name: title(locale), exact: true }) });
	await expect(card.getByRole('listitem')).toHaveCount(6);
	const sentences = await card.getByRole('listitem').allInnerTexts();
	// Formatted in the browser, whose Intl data the page uses too (Node's ICU may differ).
	const values = await page.evaluate((locale) => {
		const num = new Intl.NumberFormat(locale).format;
		const date = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' });
		return [
			date.format(new Date('2026-08-27')),
			date.format(new Date('2026-09-25')),
			// resolution median 216000 s = 60 h, over 48 h, so days
			new Intl.NumberFormat(locale, { style: 'unit', unit: 'day', unitDisplay: 'long', maximumFractionDigits: 1 }).format(2.5),
			// new, resolved, change, previous new, open, unassigned, urgent, top building, top category
			...[1284, 1201, 94, 1190, 312, 27, 9, 731, 402].map(num)
		];
	}, locale);
	return { sentences, values };
}

for (const locale of ['en', 'zh-CN', 'my', 'th']) {
	test(`written summary (${locale})`, async ({ page }) => {
		const { sentences, values } = await openSummary(page, locale);
		const text = sentences.join('\n');
		for (const v of values) expect(text).toContain(v);
		expect(text).not.toMatch(/[{}]/);
		expect(await axeViolations(page)).toEqual([]);
	});
}

// FAILS until the summary_ keys are translated (i18n-translator's follow-up to T3.02): zh-CN, my and th still hold
// the English placeholders. Do not skip it; the translations turn it green. Numbers and dates are masked first,
// because Intl already formats them per locale, so only the sentence templates are compared.
for (const locale of ['zh-CN', 'my', 'th']) {
	test(`written summary is translated (${locale})`, async ({ page }) => {
		const mask = ({ sentences, values }: { sentences: string[]; values: string[] }) => {
			const escaped = values.sort((a, b) => b.length - a.length).map((v) => v.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
			const re = new RegExp(escaped.join('|'), 'g');
			return sentences.map((s) => s.replace(re, '#'));
		};
		const en = mask(await openSummary(page, 'en'));
		const translated = mask(await openSummary(page, locale));
		translated.forEach((s, i) => expect.soft(s, `sentence ${i + 1} is still English`).not.toBe(en[i]));
	});
}
