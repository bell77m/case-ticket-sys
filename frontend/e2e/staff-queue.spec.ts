import { expect, test } from '@playwright/test';
import { axeViolations, guestTicket, signIn } from './helpers';

// T1.18: staff find a Thai ticket by a phrase from its case details; filters live in the URL.
test('queue search finds a Thai ticket, on a phone and on desktop', async ({ page, request }) => {
	const phrase = `เครื่องสแกนเนอร์ไม่ทำงาน ${Date.now()}`;
	const { id } = await guestTicket(request, `ห้องประชุมชั้นสอง ${phrase} ตั้งแต่เช้า`, 'th');
	await signIn(page, 'agent');

	await page.getByLabel('Search', { exact: true }).fill(phrase);
	await page.getByRole('button', { name: 'Search', exact: true }).click();
	await expect(page).toHaveURL(new RegExp(`q=`));
	const card = page.getByRole('listitem').filter({ hasText: `#${id}` });
	await expect(card).toBeVisible();
	await expect(card.getByRole('link')).toHaveAttribute('href', `/staff/tickets/${id}`);
	expect(await axeViolations(page)).toEqual([]);
	await page.screenshot({ path: 'test-results/queue-phone.png', fullPage: true });

	await page.setViewportSize({ width: 1280, height: 800 });
	await expect(page.getByRole('row').filter({ hasText: `#${id}` })).toBeVisible();
	expect(await axeViolations(page)).toEqual([]);
	await page.screenshot({ path: 'test-results/queue-desktop.png', fullPage: true });

	// Status "Closed" excludes the new ticket.
	await page.getByLabel('Status').selectOption('closed');
	await expect(page).toHaveURL(/status=closed/);
	await expect(page.getByRole('row').filter({ hasText: `#${id}` })).toHaveCount(0);
});
