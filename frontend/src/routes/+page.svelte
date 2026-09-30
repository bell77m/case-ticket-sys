<!-- Welcome page for guests: what the service is, how it works, one way in to the form. -->
<script lang="ts">
	import Button from '$lib/components/Button.svelte';
	import { m } from '$lib/paraglide/messages';

	const steps = [m.welcome_step_1, m.welcome_step_2, m.welcome_step_3];
</script>

<div class="hero">
	<div class="intro rise">
		<h1>{m.welcome_heading()}</h1>
		<p class="lead">{m.welcome_intro()}</p>
		<Button href="/report">{m.welcome_start()}</Button>
		<p class="hint">{m.welcome_track_hint()}</p>
	</div>

	<section class="steps rise" style:--delay="100ms" aria-labelledby="steps-title">
		<h2 id="steps-title">{m.welcome_steps()}</h2>
		<ol>
			{#each steps as step, i (i)}
				<li class="rise" style:--delay="{200 + i * 100}ms">{step()}</li>
			{/each}
		</ol>
	</section>
</div>

<style>
	.hero {
		display: grid;
		gap: var(--space-xl);
		max-width: 1120px;
		margin: 0 auto;
	}
	.intro {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-md);
	}
	p {
		margin: 0;
	}
	.hint {
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	/* Marquee card, as the tracking card after submit (DESIGN.md "Surfaces"). */
	.steps {
		padding: var(--space-lg);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-xl);
		background: var(--color-canvas);
		box-shadow: var(--shadow-md);
	}
	h2 {
		margin: 0 0 var(--space-md);
		font: var(--font-title-md);
		color: var(--color-ink);
	}
	ol {
		display: flex;
		flex-direction: column;
		gap: var(--space-md);
		margin: 0;
		padding: 0;
		list-style: none;
		counter-reset: step;
	}
	li {
		display: grid;
		grid-template-columns: var(--space-xl) 1fr;
		gap: var(--space-sm);
		counter-increment: step;
	}
	li::before {
		content: counter(step);
		display: grid;
		place-items: center;
		width: var(--space-xl);
		height: var(--space-xl);
		border-radius: var(--radius-full);
		background: var(--color-accent-tint);
		color: var(--color-ink);
		font: var(--font-label);
	}
	/* Phones: the button spans the width, in easy reach of a thumb. */
	@media (max-width: 767px) {
		.intro :global(a) {
			align-self: stretch;
		}
	}
	/* Desktop: DESIGN.md hero band, text left (7) and the card right (5). */
	@media (min-width: 1024px) {
		.hero {
			grid-template-columns: 7fr 5fr;
			align-items: center;
			gap: var(--space-xxl);
			padding: var(--space-xxl) 0;
		}
		h1 {
			font: var(--font-display-md);
			letter-spacing: var(--tracking-display);
		}
		.steps {
			padding: var(--space-xl);
		}
	}
</style>
