import { expect, test } from '@playwright/test';
import { axeViolations, signIn } from './helpers';

// T2.15: staff sign in with username and password. Needs `make seed` (dev staff, password dev-password).

// FR-A2: an unknown username gets the same translated message as a wrong password. Only one failed attempt per
// run: failures from 127.0.0.1 count toward the per-IP limit (FR-A11).
test('unknown username is refused', async ({ page }) => {
	await signIn(page, `stranger-${Date.now()}`, 'some-password-1');
	await expect(page.getByRole('alert')).toContainText('Wrong username or password.');
	await expect(page).toHaveURL(/\/login$/);
	expect(await axeViolations(page)).toEqual([]);
});

// FR-R1: a staff member signs in, sees their name and username, and signs out.
test('staff member signs in and out', async ({ page }) => {
	await signIn(page, 'agent');
	await expect(page).toHaveURL(/\/staff$/);
	// Name, username and sign-out live in the account menu, top right of every staff page.
	await page.getByRole('button', { name: 'Dev Agent' }).click();
	await expect(page.locator('#account-menu')).toContainText('agent');
	await expect(page.locator('#account-menu').getByRole('link', { name: 'Change password' })).toBeVisible();

	await page.getByRole('button', { name: 'Sign out' }).click();
	await expect(page).toHaveURL(/\/login$/);
	await page.goto('/staff');
	await expect(page).toHaveURL(/\/login$/);
});
