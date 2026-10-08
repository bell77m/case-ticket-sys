import { expect, test, type Page } from '@playwright/test';
import { axeBothSizes, signIn } from './helpers';

// T2.10, T2.15: every admin action shows only to roles that can use it (FR-A1, FR-A3, FR-A4, FR-A9). The API refuses them anyway (FR-R3).

/** Cleanup through the UI as root: deactivate the account (audit rows stay; accounts are never deleted). */
async function deactivate(page: Page, username: string, name: string) {
	await page.context().clearCookies();
	await signIn(page, 'root');
	await expect(page).toHaveURL(/\/staff$/);
	await page.goto('/staff/admin/staff');
	await expect(page.getByRole('heading', { level: 1 })).toHaveText('Staff accounts');
	const card = page.getByRole('listitem').filter({ hasText: username });
	if ((await card.count()) === 0) return; // never created
	await card.getByRole('button', { name: 'Deactivate' }).click();
	const dialog = page.getByRole('dialog');
	await expect(dialog).toContainText(`Deactivate ${name}?`);
	await dialog.getByRole('button', { name: 'Deactivate' }).click();
	// It stays in view until the page reloads; then it is behind "Show deactivated accounts". Not ticked here:
	// the dev DB holds hundreds of deactivated test accounts, which would make this step slow.
	await expect(card).toContainText('Deactivated');
	await expect(card.getByRole('button', { name: 'Reactivate' })).toBeVisible();
	await page.reload();
	await expect(page.getByRole('heading', { level: 1 })).toHaveText('Staff accounts');
	await expect(page.getByLabel(/Show deactivated accounts/)).not.toBeChecked();
	await expect(card).toHaveCount(0);
}

test('root adds an Admin, who manages staff without Root Admin powers', async ({ page }) => {
	test.slow(); // five sign-ins and five axe runs; a timeout would also skip the cleanup below
	const stamp = Date.now();
	const username = `e2e-admin-${stamp}`;
	const temp = 'e2e-temp-password';
	const own = 'e2e-own-password-1';
	const name = `E2E Admin ${stamp}`;

	try {
		// Root Admin: the account menu leads to the admin pages.
		await signIn(page, 'root');
		await expect(page).toHaveURL(/\/staff$/);
		await page.getByRole('button', { name: 'Dev Root Admin' }).click();
		await page.getByRole('link', { name: 'Staff accounts' }).click();
		await expect(page.getByRole('heading', { level: 1 })).toHaveText('Staff accounts');
		await expect(page.locator('#account-menu')).toBeHidden();

		// FR-A1: Root Admin creates the account; the Root Admin role is on offer to them.
		const add = page.locator('form').filter({ has: page.getByRole('button', { name: 'Add staff member' }) });
		await expect(add.getByLabel('Role').locator('option', { hasText: 'Root Admin' })).toHaveCount(1);
		await add.getByRole('button', { name: 'Add staff member' }).click();
		await expect(add.getByText('This field is required.')).toHaveCount(4); // name, username, role, temporary password
		await add.getByLabel('Name', { exact: true }).fill(name);
		await add.getByLabel('Username').fill(username);
		await add.getByLabel('Role').selectOption({ label: 'Admin' });
		await add.getByLabel('Temporary password').fill(temp);
		await add.getByRole('button', { name: 'Add staff member' }).click();
		const created = page.getByRole('listitem').filter({ hasText: username });
		await expect(created).toContainText('Active');
		await expect(add.getByLabel(/email/i)).toHaveCount(0); // staff have no email (2026-09-24)
		await expect(created.getByLabel('Role').locator('option:checked')).toHaveText('Admin');
		await expect(add.getByLabel('Username')).toHaveValue('');

		// The same username again, in capitals, is refused with a translated message.
		await add.getByLabel('Name', { exact: true }).fill(name);
		await add.getByLabel('Username').fill(username.toUpperCase());
		await add.getByLabel('Role').selectOption({ label: 'Admin' });
		await add.getByLabel('Temporary password').fill(temp);
		await add.getByRole('button', { name: 'Add staff member' }).click();
		await expect(add.getByText('This username is already taken.')).toBeVisible();

		// Root sees controls on their own (Root Admin) row.
		const rootCard = page.getByRole('listitem').filter({ has: page.getByText('root', { exact: true }) });
		await expect(rootCard.getByLabel('Role')).toBeVisible();
		await axeBothSizes(page, 'admin-staff-root');

		// Role editor: editable checkboxes, Save per role, "New role"; the root-only permissions stay off (FR-A3).
		await page.goto('/staff/admin/roles');
		await expect(page.getByRole('heading', { level: 1 })).toHaveText('Roles');
		const adminCard = page.getByRole('listitem').filter({ has: page.getByRole('heading', { name: 'Admin', exact: true }) });
		await expect(adminCard.getByRole('checkbox', { name: 'View all tickets' })).toBeChecked();
		await expect(adminCard.getByRole('checkbox', { name: 'View all tickets' })).toBeEnabled();
		await expect(adminCard.getByRole('checkbox', { name: /Create staff accounts/ })).toBeDisabled();
		await expect(adminCard.getByRole('button', { name: 'Save Admin' })).toBeVisible();
		const rootRole = page.getByRole('listitem').filter({ has: page.getByRole('heading', { name: 'Root Admin', exact: true }) });
		await expect(rootRole.getByRole('checkbox')).toHaveCount(0);
		await expect(rootRole).toContainText('Cannot be changed');
		await expect(page.getByRole('heading', { name: 'New role' })).toBeVisible();
		await expect(page.getByLabel('Role name')).toBeVisible();
		await axeBothSizes(page, 'admin-roles-root');

		// The new Admin signs in with the temporary password and must choose their own first (FR-A2, FR-A8).
		// A deep link does not get around it.
		await page.context().clearCookies();
		await signIn(page, username, temp);
		await expect(page).toHaveURL(/\/staff\/password$/);
		await page.goto('/staff/admin/staff');
		await expect(page).toHaveURL(/\/staff\/password$/);
		await expect(page.getByRole('status')).toContainText('temporary password');
		await axeBothSizes(page, 'change-password-forced');
		await page.getByLabel('Current password').fill(temp);
		await page.getByLabel('New password', { exact: true }).fill(own);
		await page.getByLabel('Repeat new password').fill(own);
		await page.getByRole('button', { name: 'Change password' }).click();
		await expect(page).toHaveURL(/\/staff$/);
		await page.getByRole('button', { name }).click();
		await page.getByRole('link', { name: 'Staff accounts' }).click();
		await expect(page.getByRole('heading', { level: 1 })).toHaveText('Staff accounts');

		// staff.manage: role controls on other rows. No create form (staff.create) and no Root Admin role (FR-A4).
		const agent = page.getByRole('listitem').filter({ has: page.getByText('agent', { exact: true }) });
		await expect(agent.getByLabel('Role')).toBeVisible();
		await expect(agent.getByRole('button', { name: 'Save' })).toBeVisible();
		await expect(agent.getByRole('button', { name: 'Deactivate' })).toBeVisible();
		await expect(page.getByRole('button', { name: 'Add staff member' })).toHaveCount(0);
		await expect(page.getByLabel('Temporary password')).toHaveCount(0);
		await expect(page.locator('option', { hasText: 'Root Admin' })).toHaveCount(0);
		const root = page.getByRole('listitem').filter({ has: page.getByText('root', { exact: true }) });
		await expect(root).toContainText('Root Admin');
		await expect(root.getByRole('combobox')).toHaveCount(0);
		await expect(root.getByRole('button')).toHaveCount(0);

		// The roles grid is read-only for an Admin.
		await page.goto('/staff/admin/roles');
		await expect(page.getByText('Only a Root Admin can change roles.')).toBeVisible();
		await expect(adminCard).toContainText('View all tickets');
		await expect(page.getByRole('checkbox')).toHaveCount(0);
		await expect(page.getByRole('button', { name: /^Save/ })).toHaveCount(0);
		await expect(page.getByRole('heading', { name: 'New role' })).toHaveCount(0);

		// FR-A9: Root Admin resets the new Admin's password; the next sign-in must change it again.
		await page.context().clearCookies();
		await signIn(page, 'root');
		await expect(page).toHaveURL(/\/staff$/);
		await page.goto('/staff/admin/staff');
		const row = page.getByRole('listitem').filter({ hasText: username });
		await row.getByRole('button', { name: 'Reset password' }).click();
		const reset = page.getByRole('dialog');
		await expect(reset).toContainText(`Reset password for ${name}?`);
		await reset.getByLabel('Temporary password').fill(temp);
		await reset.getByRole('button', { name: 'Reset password' }).click();
		await expect(page.getByText('Temporary password set.')).toBeVisible();
		await page.context().clearCookies();
		await signIn(page, username, temp);
		await expect(page).toHaveURL(/\/staff\/password$/);
	} finally {
		await deactivate(page, username, name);
	}
});

test('agent has no admin links and the admin pages say no permission', async ({ page }) => {
	await signIn(page, 'agent');
	await expect(page).toHaveURL(/\/staff$/);
	await page.getByRole('button', { name: 'Dev Agent' }).click();
	await expect(page.locator('#account-menu')).toContainText('agent');
	await expect(page.getByRole('link', { name: 'Staff accounts' })).toHaveCount(0);
	await expect(page.getByRole('link', { name: 'Roles' })).toHaveCount(0);

	for (const path of ['/staff/admin/staff', '/staff/admin/roles']) {
		await page.goto(path);
		await expect(page.getByRole('alert')).toContainText('You do not have permission to open this page.');
		await expect(page.getByRole('table')).toHaveCount(0);
	}
	// Hidden links are not the control: the API refuses the Agent too (FR-R3).
	expect((await page.request.get('/api/staff/accounts')).status()).toBe(403);
});

test('admin tabs switch pages, search narrows the lists, and unsaved role ticks warn before leaving', async ({ page }) => {
	await signIn(page, 'root');
	await expect(page).toHaveURL(/\/staff$/);
	await page.goto('/staff/admin/staff');
	const tabs = page.getByRole('navigation', { name: 'Admin pages' });
	await expect(tabs.getByRole('link', { name: 'Staff accounts' })).toHaveAttribute('aria-current', 'page');

	// Staff search narrows as you type, on name, username or role.
	const card = (username: string) => page.getByRole('listitem').filter({ has: page.getByText(username, { exact: true }) });
	await page.getByLabel('Search this list').fill('agent');
	await expect(card('agent')).toBeVisible();
	await expect(card('root')).toHaveCount(0);
	await page.getByLabel('Search this list').fill('no-such-person-xyz');
	await expect(page.getByText('Nothing matches your search.')).toBeVisible();

	// Roles: a changed tick shows "Unsaved changes"; leaving asks first; Discard puts the saved ticks back.
	await tabs.getByRole('link', { name: 'Roles' }).click();
	await expect(page.getByRole('heading', { level: 1 })).toHaveText('Roles');
	const admin = page.getByRole('listitem').filter({ has: page.getByRole('heading', { name: 'Admin', exact: true }) });
	const box = admin.getByRole('checkbox', { name: 'View reports' });
	const was = await box.isChecked();
	await box.click();
	await expect(admin).toContainText('Unsaved changes');
	await axeBothSizes(page, 'admin-roles-unsaved');

	let asked = '';
	page.once('dialog', (d) => {
		asked = d.message();
		return d.dismiss();
	});
	await tabs.getByRole('link', { name: 'Staff accounts' }).click();
	await expect.poll(() => asked).toContain('Leave this page?');
	await expect(page.getByRole('heading', { level: 1 })).toHaveText('Roles');

	await admin.getByRole('button', { name: 'Discard changes to Admin' }).click();
	await expect(admin).not.toContainText('Unsaved changes');
	expect(await box.isChecked()).toBe(was);

	// Nothing unsaved: the tab goes straight on. Lookups search matches any of the four names in the open tab.
	await tabs.getByRole('link', { name: 'Categories and locations' }).click();
	await expect(page.getByRole('heading', { level: 1 })).toHaveText('Categories and locations');
	await expect(tabs.getByRole('link', { name: 'Categories and locations' })).toHaveAttribute('aria-current', 'page');
	await page.getByLabel('Search this list').fill('no-such-place-xyz');
	await expect(page.getByText('Nothing matches your search.')).toBeVisible();
});
