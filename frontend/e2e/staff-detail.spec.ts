import { expect, test } from '@playwright/test';
import { axeViolations, guestTicket, signIn } from './helpers';

// A real 1x1 PNG, so the thumbnail and lightbox render.
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==', 'base64');

// T1.19: staff open a ticket from the queue and see details, evidence (lightbox) and the guest's reply.
test('ticket detail on phone and desktop', async ({ page, request }) => {
	const { id, token } = await guestTicket(request, 'The label printer on line 1 prints blank labels since Monday.');
	const up = await request.post(`/api/tickets/${id}/attachments`, {
		headers: { 'X-Tracking-Token': token },
		multipart: { file: { name: 'label.png', mimeType: 'image/png', buffer: png } }
	});
	expect(up.status()).toBe(201);
	await request.post('/api/track/comments', { headers: { 'X-Tracking-Token': token }, data: { body: 'It happened again today.' } });

	await signIn(page, 'agent');
	await page.getByLabel('Search', { exact: true }).fill(`#${id}`);
	await page.getByRole('button', { name: 'Search', exact: true }).click();
	await page.getByRole('listitem').filter({ hasText: `#${id}` }).getByRole('link').click();

	await expect(page.getByRole('heading', { level: 1 })).toHaveText(`Ticket #${id}`);
	await expect(page.getByText('prints blank labels')).toBeVisible();
	await expect(page.getByText('It happened again today.')).toBeVisible();
	expect(await axeViolations(page)).toEqual([]);
	await page.screenshot({ path: 'test-results/detail-phone.png', fullPage: true });

	await page.getByRole('button', { name: 'Open Photo 1' }).click();
	const lightbox = page.getByRole('dialog');
	await expect(lightbox.getByRole('img', { name: 'Photo 1' })).toBeVisible();
	await page.keyboard.press('Escape');
	await expect(lightbox).toBeHidden();

	await page.setViewportSize({ width: 1280, height: 800 });
	expect(await axeViolations(page)).toEqual([]);
	await page.screenshot({ path: 'test-results/detail-desktop.png', fullPage: true });
});

test('unknown ticket shows not found', async ({ page }) => {
	await signIn(page, 'viewer');
	await page.goto('/staff/tickets/999999999');
	await expect(page.getByRole('alert')).toContainText('does not exist');
});
