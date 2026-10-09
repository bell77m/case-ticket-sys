import { expect, test } from '@playwright/test';
import { baseURL } from './helpers';

const jpeg = Buffer.concat([Buffer.from([0xff, 0xd8, 0xff, 0xe0, 0, 0x10]), Buffer.from('JFIF'), Buffer.alloc(2000, 1)]);
const mp4 = Buffer.concat([Buffer.from([0, 0, 0, 0x18]), Buffer.from('ftypisom'), Buffer.alloc(3000, 2)]);

// T1.13, FR-G1, FR-T1: a guest submits a ticket with a photo and a video from a phone-sized screen.
test('guest submits a ticket with photo and video', async ({ page }) => {
	await page.goto('/report');
	await page.getByLabel('Your name').fill('Aye Aye');
	await page.getByLabel('Employee ID').fill('E1001');
	await page.getByLabel('Building').selectOption({ index: 1 });
	await page.getByLabel('Floor').selectOption({ index: 1 });
	await page.getByLabel('Line').selectOption({ index: 1 });
	await page.getByLabel('What is the problem?').fill('The printer on line 1 jams every morning.');
	await page.getByLabel('Add photos or videos').setInputFiles([
		{ name: 'photo.jpg', mimeType: 'image/jpeg', buffer: jpeg },
		{ name: 'clip.mp4', mimeType: 'video/mp4', buffer: mp4 }
	]);
	await expect(page.getByRole('listitem').filter({ hasText: 'photo.jpg' })).toBeVisible();
	await expect(page.getByRole('listitem').filter({ hasText: 'clip.mp4' })).toBeVisible();

	await page.getByRole('button', { name: 'Submit ticket' }).click();

	// Focus moves to the success heading, so screen readers announce it and phones scroll to it.
	await expect(page.getByRole('heading', { name: /Ticket #\d+ created/ })).toBeFocused();
	await expect(page.getByText('2 files attached')).toBeVisible();
});

// NFR-5: the server's YARA scan (clamd, deploy/base/clamav) refuses a photo with a PHP web shell inside; the clean
// photo is kept and the guest is told which file was blocked.
test('photo with a hidden script is blocked', async ({ page }) => {
	await page.goto('/report');
	await page.getByLabel('Your name').fill('Mya Mya');
	await page.getByLabel('Employee ID').fill('E5005');
	await page.getByLabel('Building').selectOption({ index: 1 });
	await page.getByLabel('Floor').selectOption({ index: 1 });
	await page.getByLabel('Line').selectOption({ index: 1 });
	await page.getByLabel('What is the problem?').fill('Scanner on line 1 shows an error code.');
	await page.getByLabel('Add photos or videos').setInputFiles([
		{ name: 'clean.jpg', mimeType: 'image/jpeg', buffer: jpeg },
		{ name: 'shell.jpg', mimeType: 'image/jpeg', buffer: Buffer.concat([jpeg, Buffer.from("<?php system($_GET['c']); ?>")]) }
	]);
	await page.getByRole('button', { name: 'Submit ticket' }).click();

	await expect(page.getByRole('heading', { name: /Ticket #\d+ created/ })).toBeFocused();
	await expect(page.getByText('1 file attached')).toBeVisible();
	await expect(page.getByText('shell.jpg was blocked by the security check.', { exact: false })).toBeVisible();
});

// The welcome page leads guests to the form; the brand link brings them back.
test('welcome page opens the form', async ({ page }) => {
	await page.goto('/');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await page.getByRole('link', { name: 'Report a problem' }).click();
	await expect(page).toHaveURL(/\/report$/);
	await expect(page.getByLabel('Your name')).toBeVisible();
});

// After submit the guest can start a clean form for another problem, or go home.
test('success page offers another report and home', async ({ page }) => {
	await page.goto('/report');
	await page.getByLabel('Your name').fill('Ko Ko');
	await page.getByLabel('Employee ID').fill('E4004');
	await page.getByLabel('Building').selectOption({ index: 1 });
	await page.getByLabel('Floor').selectOption({ index: 1 });
	await page.getByLabel('Line').selectOption({ index: 1 });
	await page.getByLabel('What is the problem?').fill('Monitor on line 1 flickers all day.');
	await page.getByRole('button', { name: 'Submit ticket' }).click();

	await expect(page.getByRole('link', { name: 'Back to home' })).toHaveAttribute('href', '/');
	await page.getByRole('button', { name: 'Report another problem' }).click();
	await expect(page.getByLabel('Your name')).toHaveValue('');
	await expect(page.getByLabel('What is the problem?')).toHaveValue('');
	await expect(page.getByLabel('Floor')).toBeDisabled();
	await expect(page.getByLabel('Your name')).not.toHaveAttribute('aria-invalid', 'true');
});

// Inline errors: empty submit marks every required field, and floor/line wait for the level above.
test('empty form shows inline errors', async ({ page }) => {
	await page.goto('/report');
	await expect(page.getByLabel('Floor')).toBeDisabled();
	await page.getByRole('button', { name: 'Submit ticket' }).click();
	await expect(page.getByLabel('Your name')).toHaveAttribute('aria-invalid', 'true');
	await expect(page.getByLabel('Employee ID')).toHaveAttribute('aria-invalid', 'true');
	await expect(page.getByLabel('What is the problem?')).toHaveAttribute('aria-invalid', 'true');
});

// FR-T1: a file that is too large is refused before upload, with a message.
test('oversize image is refused in the browser', async ({ page }) => {
	await page.goto('/report');
	await page.getByLabel('Add photos or videos').setInputFiles({
		name: 'huge.jpg',
		mimeType: 'image/jpeg',
		buffer: Buffer.concat([jpeg, Buffer.alloc(10 * 1024 * 1024)])
	});
	await expect(page.getByText(/huge\.jpg.*10 MB/)).toBeVisible();
});

// The location error follows the first dropdown still to choose.
test('location error moves to the missing level', async ({ page }) => {
	await page.goto('/report');
	await page.getByLabel('Building').selectOption({ index: 1 });
	await page.getByLabel('Floor').selectOption({ index: 1 });
	await page.getByRole('button', { name: 'Submit ticket' }).click();
	await expect(page.getByLabel('Line')).toHaveAttribute('aria-invalid', 'true');
	await expect(page.getByLabel('Building')).not.toHaveAttribute('aria-invalid', 'true');
});

// T1.14, FR-G2: after submit the guest sees the ticket number, a copyable tracking link and a QR code of it.
test('tracking card shows link, copy button and QR code', async ({ page, context }) => {
	await context.grantPermissions(['clipboard-read', 'clipboard-write']);
	await page.goto('/report');
	await page.getByLabel('Your name').fill('Min Min');
	await page.getByLabel('Employee ID').fill('E2002');
	await page.getByLabel('Building').selectOption({ index: 1 });
	await page.getByLabel('Floor').selectOption({ index: 1 });
	await page.getByLabel('Line').selectOption({ index: 1 });
	await page.getByLabel('What is the problem?').fill('Scanner does not turn on at all.');
	await page.getByRole('button', { name: 'Submit ticket' }).click();

	const link = page.getByLabel('Your tracking link');
	const origin = baseURL.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&');
	await expect(link).toHaveValue(new RegExp(`^${origin}/track#[A-Za-z0-9_-]{43}$`));
	const url = await link.inputValue();

	await page.getByRole('button', { name: 'Copy link' }).click();
	await expect(page.getByText('Link copied')).toBeVisible();
	expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(url);

	// The QR image must encode exactly the tracking link.
	const src = await page.getByRole('img', { name: 'QR code for your tracking link' }).getAttribute('src');
	const QRCode = (await import('qrcode')).default;
	expect(src).toBe('data:image/svg+xml,' + encodeURIComponent(await QRCode.toString(url, { type: 'svg', margin: 1 })));

	await expect(page.getByText('Save this link. It is the only way to view your ticket.')).toBeVisible();
});
