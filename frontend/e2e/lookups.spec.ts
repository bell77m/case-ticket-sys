import { expect, test, type Page } from '@playwright/test';
import { baseURL, axeBothSizes, axeViolations, signIn } from './helpers';

// T2.11 (FR-I4): a line added in the admin page shows in the guest form in all four languages; a deactivated line
// leaves it. Each run adds its own building and floor, so it cannot clash with seed data. The line is left
// deactivated: there is no delete, by design.

type Names = Record<'en' | 'zh-CN' | 'my' | 'th', string>;
const labels: Names = { en: 'English', 'zh-CN': '中文', my: 'မြန်မာ', th: 'ไทย' };

async function fillNames(page: Page, legend: string, names: Names) {
	const group = page.getByRole('group', { name: legend, exact: true });
	for (const l of Object.keys(labels) as (keyof Names)[]) await group.getByLabel(labels[l], { exact: true }).fill(names[l]);
}

/** The guest form in one language; returns its building, floor and line selects. */
async function openReport(page: Page, locale: string) {
	await page.context().addCookies([{ name: 'PARAGLIDE_LOCALE', value: locale, url: baseURL }]);
	await page.goto('/report');
	await expect(page.locator('html')).toHaveAttribute('lang', locale);
	return page.locator('select');
}

test('a new line shows in the guest form in every language until it is deactivated', async ({ page }) => {
	test.slow(); // four guest form loads and two axe runs
	const s = Date.now();
	const building: Names = { en: `E2E Building ${s}`, 'zh-CN': `E2E 大楼 ${s}`, my: `E2E အဆောက်အအုံ ${s}`, th: `E2E อาคาร ${s}` };
	const floor: Names = { en: `E2E Floor ${s}`, 'zh-CN': `E2E 楼层 ${s}`, my: `E2E အထပ် ${s}`, th: `E2E ชั้น ${s}` };
	const line: Names = { en: `E2E Line ${s}`, 'zh-CN': `E2E 线 ${s}`, my: `E2E လိုင်း ${s}`, th: `E2E ไลน์ ${s}` };

	await signIn(page, 'root');
	await expect(page).toHaveURL(/\/staff$/);
	try {
		await page.getByRole('button', { name: 'Dev Root Admin' }).click();
		await page.getByRole('link', { name: 'Categories and locations' }).click();
		await expect(page.getByRole('heading', { level: 1 })).toHaveText('Categories and locations');
		await expect(page.locator('#account-menu')).toBeHidden();

		// One list at a time: the Locations tab, kept in the URL. Its add form opens in a dialog.
		await page.getByRole('link', { name: /^Locations/ }).click();
		await expect(page).toHaveURL(/tab=locations/);
		await expect(page.getByRole('button', { name: 'Add line', exact: true })).toBeHidden();
		await page.getByRole('button', { name: 'Add a line', exact: true }).click();

		// Nothing picked: the building select says so.
		// Located by its title, so it still resolves once the dialog is closed (getByRole skips hidden elements).
		const add = page.locator('dialog', { has: page.locator('#add-title') }).locator('form');
		const pickBuilding = add.getByLabel('Building', { exact: true });
		await add.getByRole('button', { name: 'Add line' }).click();
		await expect(pickBuilding).toHaveAttribute('aria-invalid', 'true');

		// A new building shows its names and a new floor, with no floor select.
		await pickBuilding.selectOption({ label: 'New building' });
		await expect(add.getByLabel('Floor', { exact: true })).toHaveCount(0);
		await fillNames(page, 'New building', building);
		await fillNames(page, 'New floor', floor);

		// All four languages are required; the API names the missing one.
		await fillNames(page, 'New line', { ...line, th: '' });
		await add.getByRole('button', { name: 'Add line' }).click();
		const lineFields = page.getByRole('group', { name: 'New line', exact: true });
		await expect(lineFields.getByText('This field is required.')).toBeVisible();
		await expect(lineFields.getByLabel('ไทย', { exact: true })).toHaveAttribute('aria-invalid', 'true');
		await expect(lineFields.getByLabel('ไทย', { exact: true })).toHaveAttribute('lang', 'th');
		await lineFields.getByLabel('ไทย', { exact: true }).fill(line.th);
		await add.getByRole('button', { name: 'Add line' }).click();

		// Saved: the dialog closes and the line shows as a card (phone size).
		await expect(page.getByRole('dialog')).toBeHidden();
		const row = page.getByRole('listitem').filter({ hasText: line.en });
		await expect(row).toContainText('Active');
		await expect(row).toContainText(line.th);
		await expect(pickBuilding).toHaveValue('');

		// Edit opens the names in a dialog; Save closes it and focus goes back to Edit.
		const editButton = row.getByRole('button', { name: 'Edit' });
		await editButton.click();
		const editDialog = page.getByRole('dialog');
		await expect(editDialog).toContainText(`Edit ${building.en} / ${floor.en} / ${line.en}`);
		expect(await axeViolations(page)).toEqual([]);
		line['zh-CN'] = `E2E 线路 ${s}`;
		await editDialog.getByRole('group', { name: 'Line', exact: true }).getByLabel('中文', { exact: true }).fill(line['zh-CN']);
		await editDialog.getByRole('button', { name: 'Save' }).click();
		await expect(row).toContainText(line['zh-CN']);
		await expect(editDialog).toBeHidden();
		await expect(editButton).toBeFocused();
		await axeBothSizes(page, 'admin-lookups');

		// FR-I4: the guest form shows the names in each language.
		for (const locale of Object.keys(labels) as (keyof Names)[]) {
			const selects = await openReport(page, locale);
			await selects.nth(0).selectOption({ label: building[locale] });
			await selects.nth(1).selectOption({ label: floor[locale] });
			await selects.nth(2).selectOption({ label: line[locale] });
			await expect(selects.nth(2).locator('option:checked')).toHaveText(line[locale]);
		}

		// Deactivated: the line stays in the admin list for this visit, muted, and leaves the guest form.
		await page.context().addCookies([{ name: 'PARAGLIDE_LOCALE', value: 'en', url: baseURL }]);
		await page.goto('/staff/admin/lookups?tab=locations');
		await row.getByRole('button', { name: 'Deactivate' }).click();
		const dialog = page.getByRole('dialog');
		await expect(dialog).toContainText(`Deactivate ${building.en} / ${floor.en} / ${line.en}?`);
		await dialog.getByRole('button', { name: 'Deactivate' }).click();
		await expect(row).toContainText('Deactivated');
		await expect(row.getByRole('button', { name: 'Reactivate' })).toBeVisible();

		// Deactivated rows pile up, so the next visit hides them until "Show deactivated" is ticked.
		await page.reload();
		await expect(page.getByRole('heading', { level: 1 })).toHaveText('Categories and locations');
		await expect(row).toHaveCount(0);
		await expect(pickBuilding.locator('option', { hasText: building.en })).toHaveCount(0);
		await page.getByLabel(/Show deactivated categories and lines/).check();
		await expect(row).toContainText('Deactivated');

		const selects = await openReport(page, 'en');
		await expect(selects.nth(0).locator('option', { hasText: 'Building A' })).toHaveCount(1); // loaded
		await expect(selects.nth(0).locator('option', { hasText: building.en })).toHaveCount(0);
	} finally {
		// Leave the line deactivated even when a step above failed.
		const rows: { id: number; line: Names; is_active: boolean }[] = await (await page.request.get('/api/staff/locations')).json();
		for (const l of rows.filter((r) => r.line.en === line.en && r.is_active)) {
			await page.request.patch(`/api/staff/locations/${l.id}`, { data: { is_active: false } });
		}
	}
});

test('agent has no lookups link and the page says no permission', async ({ page }) => {
	await signIn(page, 'agent');
	await expect(page).toHaveURL(/\/staff$/);
	await page.getByRole('button', { name: 'Dev Agent' }).click();
	await expect(page.locator('#account-menu')).toContainText('agent');
	await expect(page.getByRole('link', { name: 'Categories and locations' })).toHaveCount(0);

	await page.goto('/staff/admin/lookups');
	await expect(page.getByRole('alert')).toContainText('You do not have permission to open this page.');
	await expect(page.getByRole('button', { name: 'Add line' })).toHaveCount(0);
	// Hidden links are not the control: the API refuses the Agent too (FR-R3).
	expect((await page.request.get('/api/staff/locations')).status()).toBe(403);
});
