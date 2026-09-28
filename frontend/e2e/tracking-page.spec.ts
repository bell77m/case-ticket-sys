import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';

const axeSource = readFileSync('node_modules/axe-core/axe.min.js', 'utf8');

const jpeg = Buffer.concat([Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0, 0x10]), Buffer.from('JFIF'), Buffer.alloc(2000, 1)]);

async function submitTicket(page: Page, withPhoto: boolean) {
	await page.goto('/report');
	await page.getByLabel('Your name').fill('Su Su');
	await page.getByLabel('Employee ID').fill('E3003');
	await page.getByLabel('Building').selectOption({ index: 1 });
	await page.getByLabel('Floor').selectOption({ index: 1 });
	await page.getByLabel('Line').selectOption({ index: 1 });
	await page.getByLabel('What is the problem?').fill('The barcode reader beeps but sends nothing.');
	if (withPhoto) await page.getByLabel('Add photos or videos').setInputFiles({ name: 'reader.jpg', mimeType: 'image/jpeg', buffer: jpeg });
	await page.getByRole('button', { name: 'Submit ticket' }).click();
	return page.getByLabel('Your tracking link').inputValue();
}

// T1.15, FR-G3: the tracking link shows the ticket, its evidence, and lets the guest reply.
test('guest opens the tracking link, views evidence and replies', async ({ page }) => {
	const url = await submitTicket(page, true);
	await page.goto(url);

	await expect(page.getByRole('heading', { name: /Ticket #\d+/ })).toBeVisible();
	await expect(page.getByText('New', { exact: true })).toBeVisible();
	await expect(page.getByText('The barcode reader beeps but sends nothing.')).toBeVisible();
	await expect(page.getByText('No replies yet.')).toBeVisible();

	await page.getByRole('button', { name: 'View photo 1' }).click();
	await expect(page.getByRole('img', { name: 'Photo 1' })).toBeVisible();

	await page.getByLabel('Write a reply').fill('It happened again this morning.');
	await page.getByRole('button', { name: 'Send reply' }).click();
	const reply = page.getByRole('listitem').filter({ hasText: 'It happened again this morning.' });
	await expect(reply).toBeVisible();
	await expect(reply).toContainText('You');
	await expect(page.getByLabel('Write a reply')).toHaveValue('');

	// The reply survives a reload: it came from the server, not only the page state.
	await page.reload();
	await expect(page.getByText('It happened again this morning.')).toBeVisible();

	// DESIGN.md "Accessibility": no WCAG 2.2 AA violations on the tracking page.
	await page.addScriptTag({ content: axeSource });
	const violations = await page.evaluate(async () => {
		// @ts-expect-error axe is injected above
		const r = await window.axe.run(document, { runOnly: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'] });
		return r.violations.map((v: { id: string; nodes: unknown[] }) => `${v.id} (${v.nodes.length})`);
	});
	expect(violations).toEqual([]);
	await page.screenshot({ path: 'test-results/tracking-page.png', fullPage: true });
});

// FR-G4: a broken or missing token shows a clear message, not an error page.
test('broken tracking link shows not found', async ({ page }) => {
	await page.goto('/track#not-a-real-token');
	await expect(page.getByText('We could not find this ticket.')).toBeVisible();
	await page.goto('/track');
	await expect(page.getByText('We could not find this ticket.')).toBeVisible();
	// A malformed escape (a truncated link) must not break the page.
	await page.goto('/');
	await page.goto('/track#%E0%A4');
	await expect(page.getByText('We could not find this ticket.')).toBeVisible();
});
