import { expect, type APIRequestContext, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';

/** The password `make seed` gives the dev staff root, agent and viewer. */
export const devPassword = 'dev-password';

/** Signs in on /login and waits for the API's answer, so a following page.goto cannot cancel it. Dev staff come from `make seed`. */
export async function signIn(page: Page, username: string, password = devPassword) {
	await page.goto('/login');
	await page.getByLabel('Username').fill(username);
	await page.getByLabel('Password').fill(password);
	const answer = page.waitForResponse((r) => r.url().endsWith('/api/auth/login'));
	await page.getByRole('button', { name: 'Sign in' }).click();
	await answer;
}

/** Opens a guest ticket through the API and returns its number and tracking token. */
export async function guestTicket(request: APIRequestContext, caseDetails: string, language = 'en') {
	const buildings = await (await request.get('/api/locations')).json();
	const res = await request.post('/api/tickets', {
		data: {
			guest_name: 'Test Guest',
			employee_id: 'E9000',
			location_id: buildings[0].floors[0].lines[0].id,
			case_details: caseDetails,
			language
		}
	});
	const body = await res.json();
	return { id: body.ticket_id as number, token: body.tracking_token as string };
}

/** Gives every graph data today: an urgent ticket assigned to the signed-in staff member, with a public reply. */
export async function ticketForEveryGraph(page: Page, request: APIRequestContext) {
	const { id } = await guestTicket(request, 'The barcode scanner at the packing line stopped reading labels.');
	const me = await (await page.request.get('/api/auth/me')).json();
	expect((await page.request.patch(`/api/staff/tickets/${id}`, { data: { priority: 'urgent' } })).ok()).toBe(true);
	expect((await page.request.put(`/api/staff/tickets/${id}/assignee`, { data: { assignee_id: me.id } })).ok()).toBe(true);
	const reply = await page.request.post(`/api/staff/tickets/${id}/comments`, { data: { body: 'Looking into it.', internal: false } });
	expect(reply.ok()).toBe(true);
}

const axeSource = readFileSync('node_modules/axe-core/axe.min.js', 'utf8');

/** Runs axe-core (WCAG 2.2 AA) on the current page and returns "rule (nodes)" per violation. */
export async function axeViolations(page: Page) {
	await page.addScriptTag({ content: axeSource });
	return page.evaluate(async () => {
		// @ts-expect-error axe is injected above
		const r = await window.axe.run(document, { runOnly: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'] });
		return r.violations.map((v: { id: string; nodes: unknown[] }) => `${v.id} (${v.nodes.length})`);
	});
}

/** Axe at phone size, then at desktop size (the admin pages change layout at 1024px), then back to phone. */
export async function axeBothSizes(page: Page, name: string) {
	expect(await axeViolations(page)).toEqual([]);
	await page.screenshot({ path: `test-results/${name}-phone.png`, fullPage: true });
	await page.setViewportSize({ width: 1280, height: 800 });
	expect(await axeViolations(page)).toEqual([]);
	await page.screenshot({ path: `test-results/${name}-desktop.png`, fullPage: true });
	await page.setViewportSize({ width: 390, height: 844 });
}
