import { expect, test } from '@playwright/test';
import { axeViolations } from './helpers';

// FR-I2: the language button opens a menu of endonyms; picking one reloads the page in that language.
test('language menu switches the page language', async ({ page }) => {
	await page.goto('/');
	await page.getByRole('heading', { level: 1 }).waitFor();
	await page.getByRole('button', { name: /English/ }).click();
	const menu = page.locator('#lang-menu');
	await expect(menu.getByRole('button', { name: 'English' })).toHaveAttribute('aria-current', 'true');
	expect(await axeViolations(page)).toEqual([]);

	await menu.getByRole('button', { name: 'ไทย' }).click();
	await expect(page.locator('html')).toHaveAttribute('lang', 'th');
	await page.getByRole('button', { name: /ไทย/ }).click();
	await expect(menu.getByRole('button', { name: 'ไทย' })).toHaveAttribute('aria-current', 'true');
	await expect(menu.getByRole('button', { name: 'English' })).not.toHaveAttribute('aria-current');
});
