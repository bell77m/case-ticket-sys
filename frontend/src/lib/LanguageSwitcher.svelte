<script lang="ts" module>
	import type { Locale } from '$lib/paraglide/runtime';

	// Endonyms: each language is shown in its own script, identical in every locale, so not translated.
	export const languageNames: Record<Locale, string> = {
		en: 'English',
		'zh-CN': '中文',
		my: 'မြန်မာ',
		th: 'ไทย'
	};
</script>

<script lang="ts">
	import { getLocale, locales, setLocale } from '$lib/paraglide/runtime';
	import { m } from '$lib/paraglide/messages';

	// setLocale reloads the page, so the current locale never changes while this component lives.
	const current = getLocale();
</script>

<!-- Pill that opens a native popover, like the account menu: Esc and a click outside close it, focus returns to the button. -->
<!-- setLocale stores the choice in a cookie and reloads the page in that language (FR-I2). -->
<button class="lang" popovertarget="lang-menu">
	<span class="visually-hidden">{m.language_label()}</span>
	<svg class="icon" viewBox="0 0 24 24" aria-hidden="true">
		<circle cx="12" cy="12" r="10" />
		<path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20M2 12h20" />
	</svg>
	<span>{languageNames[current]}</span>
	<svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
</button>
<ul id="lang-menu" class="menu" popover>
	{#each locales as l (l)}
		<li>
			<!-- lang: screen readers pronounce each endonym in its own language, and the :lang() type rules apply. -->
			<button lang={l} aria-current={l === current ? 'true' : undefined} onclick={() => setLocale(l)}>
				<svg class="check" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 6 9 17l-5-5" /></svg>
				{languageNames[l]}
			</button>
		</li>
	{/each}
</ul>

<style>
	svg {
		flex: none;
		width: var(--space-md);
		height: var(--space-md);
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.icon {
		color: var(--color-body);
	}
	.lang {
		anchor-name: --lang;
		display: inline-flex;
		align-items: center;
		gap: var(--space-xs);
		min-height: var(--size-control);
		padding: 0 var(--space-sm);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-pill);
		background: var(--color-canvas);
		color: var(--color-ink);
		font: var(--font-nav-link);
		white-space: nowrap;
		cursor: pointer;
	}
	.lang:active {
		background: var(--color-surface-soft);
	}
	.menu {
		position-anchor: --lang;
		position-area: bottom span-left;
		inset: auto;
		min-width: 160px;
		margin: var(--space-xs) 0 0;
		padding: var(--space-xxs);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-lg);
		background: var(--color-canvas);
		box-shadow: var(--shadow-md);
		list-style: none;
	}
	.menu button {
		display: flex;
		align-items: center;
		gap: var(--space-xs);
		width: 100%;
		min-height: var(--size-control);
		padding: 0 var(--space-sm);
		border: 0;
		border-radius: var(--radius-md);
		background: none;
		color: var(--color-ink);
		font: var(--font-body-md);
		text-align: start;
		cursor: pointer;
	}
	.menu button:hover {
		background: var(--color-surface-soft);
	}
	.menu button:active {
		background: var(--color-surface-card);
	}
	/* Every row keeps the check's space so the names line up; only the current one shows it. */
	.check {
		visibility: hidden;
	}
	[aria-current] .check {
		visibility: visible;
	}
</style>
