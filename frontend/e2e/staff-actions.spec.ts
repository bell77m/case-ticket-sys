import { expect, test, type Page } from '@playwright/test';
import { axeViolations, guestTicket, signIn } from './helpers';

async function openTicket(page: Page, username: string, id: number) {
	await signIn(page, username);
	await expect(page).toHaveURL(/\/staff$/);
	await page.goto(`/staff/tickets/${id}`);
	await expect(page.getByRole('heading', { level: 1 })).toHaveText(`Ticket #${id}`);
}

// T2.06 (FR-L3): an agent works a ticket from the detail page; the timeline shows who moved it and who closed it.
test('agent works a ticket, timeline shows who moved and who closed it', async ({ page, request }) => {
	const { id, token } = await guestTicket(request, 'The barcode scanner on line 2 beeps but reads nothing.');
	await openTicket(page, 'agent', id);
	const details = page.getByRole('region', { name: 'Details' });
	const thread = page.getByRole('region', { name: 'Conversation' });
	const timeline = page.getByRole('region', { name: 'Timeline' });
	await expect(timeline).toContainText('Guest opened the ticket');

	// An Agent may assign only themselves, so there is a button, not an assignee select.
	await expect(details.getByLabel('Assignee', { exact: true })).toHaveCount(0);
	await details.getByRole('button', { name: 'Assign to me' }).click();
	await expect(timeline).toContainText('Dev Agent assigned the ticket to Dev Agent');
	await expect(details.getByRole('button', { name: 'Assign to me' })).toHaveCount(0);

	await details.getByLabel('Status', { exact: true }).selectOption('in_progress');
	await details.getByRole('button', { name: 'Save changes' }).click();
	await expect(timeline).toContainText('from New to In Progress');

	await page.getByRole('radio', { name: 'Internal note' }).check();
	await page.getByLabel('Message', { exact: true }).fill('Scanner firmware is out of date.');
	await page.getByRole('button', { name: 'Add internal note' }).click();
	await expect(thread.getByRole('listitem').filter({ hasText: 'Scanner firmware' })).toContainText('Internal note');
	await expect(page.getByLabel('Message', { exact: true })).toHaveValue('');

	await page.getByRole('radio', { name: 'Public reply' }).check();
	await page.getByLabel('Message', { exact: true }).fill('We updated the scanner. Please try again.');
	await page.getByRole('button', { name: 'Send reply' }).click();
	await expect(thread).toContainText('We updated the scanner.');
	await expect(timeline).toContainText('Dev Agent added an internal note');
	await expect(timeline).toContainText('Dev Agent added a reply');

	await details.getByLabel('Status', { exact: true }).selectOption('resolved');
	await details.getByRole('button', { name: 'Save changes' }).click();
	await expect(timeline).toContainText('from In Progress to Resolved');

	// Axe with every control on screen, the reply box in internal mode.
	await page.getByRole('radio', { name: 'Internal note' }).check();
	expect(await axeViolations(page)).toEqual([]);
	await page.setViewportSize({ width: 1280, height: 800 });
	expect(await axeViolations(page)).toEqual([]);
	await page.screenshot({ path: 'test-results/actions-desktop.png', fullPage: true });
	await page.setViewportSize({ width: 390, height: 844 });

	// A Viewer sees the same ticket without action selects or reply box (display only; the API refuses anyway).
	await page.context().clearCookies();
	await openTicket(page, 'viewer', id);
	await expect(page.getByRole('combobox')).toHaveCount(0);
	await expect(page.getByRole('textbox')).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Assign to me' })).toHaveCount(0);

	// The guest confirms the fix, which closes the ticket.
	const res = await request.post('/api/track/confirm', { headers: { 'X-Tracking-Token': token } });
	expect(res.ok()).toBe(true);
	await page.reload();
	const rows = timeline.getByRole('listitem');
	await expect(rows.filter({ hasText: 'to In Progress' })).toContainText('Dev Agent');
	await expect(rows.filter({ hasText: 'to Closed' })).toContainText('Guest');
	expect(await axeViolations(page)).toEqual([]);
});

// FR-T5: staff may set any status, including closing a ticket and reopening a closed one.
test('agent closes a ticket and reopens it', async ({ page, request }) => {
	const { id } = await guestTicket(request, 'The label printer in building B prints blank labels.');
	await openTicket(page, 'agent', id);
	const details = page.getByRole('region', { name: 'Details' });
	const timeline = page.getByRole('region', { name: 'Timeline' });
	const status = details.getByLabel('Status', { exact: true });
	await expect(status.getByRole('option')).toHaveText(['New', 'In Progress', 'Waiting on User', 'Resolved', 'Closed']);

	await status.selectOption('closed');
	await details.getByRole('button', { name: 'Save changes' }).click();
	await expect(timeline).toContainText('from New to Closed');

	await status.selectOption('in_progress');
	await details.getByRole('button', { name: 'Save changes' }).click();
	await expect(timeline).toContainText('from Closed to In Progress');
});
