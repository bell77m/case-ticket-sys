// Written summary of the reports dashboard (FR-P1): fixed sentence templates, no AI, in the viewer's language
// (FR-I6). Every sentence is one whole message; numbers and dates are formatted here with Intl (FR-I5).
import type { Report } from '$lib/api';
import { m } from '$lib/paraglide/messages';
import { getLocale } from '$lib/paraglide/runtime';

/** A median as minutes under 2 hours, hours under 48 hours, days above. `signDisplay` 'exceptZero' shows a change (+2 hours). */
export function formatDuration(seconds: number, locale: string, signDisplay: 'auto' | 'exceptZero' = 'auto') {
	const min = Math.abs(seconds) / 60;
	// Under a minute reads as 1 minute; "0 minutes" would say there was no wait at all.
	const [value, unit] = min < 120 ? [Math.max(1, Math.round(min)), 'minute'] : min < 48 * 60 ? [min / 60, 'hour'] : [min / 1440, 'day'];
	const opts = { style: 'unit', unit, unitDisplay: 'long', maximumFractionDigits: 1, signDisplay } as const;
	return new Intl.NumberFormat(locale, opts).format(seconds < 0 ? -value : value);
}

export function writtenSummary(r: Report): string[] {
	const locale = getLocale();
	const num = new Intl.NumberFormat(locale).format;
	// Period dates are plain YYYY-MM-DD, which Date reads as UTC midnight; format them in UTC so they never shift a day.
	// Gregorian in every language, like the rest of the app (Thai would default to the Buddhist era).
	const date = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC', calendar: 'gregory' });
	const c = r.cards;
	const from = date.format(new Date(r.period.from));
	const to = date.format(new Date(r.period.to));

	if (c.new.value === 0 && c.open.value === 0 && c.resolved.value === 0) return [m.summary_empty({ from, to })];

	const out = [
		m.summary_volume({
			from,
			to,
			opened: num(c.new.value),
			openedCount: c.new.value,
			resolved: num(c.resolved.value),
			resolvedCount: c.resolved.value
		})
	];

	const diff = c.new.value - c.new.previous;
	const change = { diff: num(Math.abs(diff)), previous: num(c.new.previous) };
	out.push(diff > 0 ? m.summary_change_up(change) : diff < 0 ? m.summary_change_down(change) : m.summary_change_same());

	out.push(
		c.open.value === 0
			? m.summary_open_none()
			: m.summary_open({
					open: num(c.open.value),
					openCount: c.open.value,
					unassigned: num(c.unassigned.value),
					urgent: num(c.urgent_open.value)
				})
	);

	const first = c.first_response_median_seconds.value;
	out.push(first === null ? m.summary_first_response_none() : m.summary_first_response({ duration: formatDuration(first, locale) }));
	const resolution = c.resolution_median_seconds.value;
	out.push(
		resolution === null ? m.summary_resolution_none() : m.summary_resolution({ duration: formatDuration(resolution, locale) })
	);

	// Both lists come sorted by count. Tickets without a category are not a category, so they name no top one.
	const cat = r.by_category[0];
	const bld = r.by_location[0];
	const category = cat?.name ? { category: cat.name, categoryCount: num(cat.count) } : undefined;
	const building = bld && { building: bld.building, buildingCount: num(bld.count) };
	if (category && building) out.push(m.summary_top_both({ ...category, ...building }));
	else if (building) out.push(m.summary_top_building(building));
	else if (category) out.push(m.summary_top_category(category));

	return out;
}
