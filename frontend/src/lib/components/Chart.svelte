<!-- One Chart.js chart (DESIGN.md "Reports dashboard"). Fills its parent, which sets the height. -->
<script lang="ts" module>
	import {
		ArcElement,
		BarController,
		BarElement,
		CategoryScale,
		Chart,
		DoughnutController,
		Legend,
		LinearScale,
		LineController,
		LineElement,
		PointElement,
		Tooltip,
		type ChartConfiguration
	} from 'chart.js';
	import { getLocale } from '$lib/paraglide/runtime';

	// Tree-shaken: only the charts the dashboard draws.
	Chart.register(LineController, LineElement, PointElement, BarController, BarElement, DoughnutController, ArcElement, CategoryScale, LinearScale, Tooltip, Legend);

	/** A design token's value. Canvas cannot read CSS variables, so colors come from tokens.css through here. */
	export const cssVar = (name: string) => getComputedStyle(document.documentElement).getPropertyValue(name).trim();

	/** DESIGN.md marks for every chart: tokens for text, grid and tooltip; 2px lines; 8px hover markers; 4px bar ends. */
	function applyTheme() {
		const d = Chart.defaults;
		const canvas = cssVar('--color-canvas');
		d.locale = getLocale(); // Intl number format for ticks and tooltips
		d.font.family = cssVar('--font-family');
		d.color = cssVar('--color-muted'); // axis text
		d.borderColor = cssVar('--color-hairline'); // solid grid
		d.maintainAspectRatio = false;
		if (matchMedia('(prefers-reduced-motion: reduce)').matches) d.animation = false;
		d.plugins.legend.labels.color = cssVar('--color-ink');
		d.plugins.legend.labels.boxWidth = 12;
		d.plugins.tooltip.backgroundColor = cssVar('--color-surface-dark');
		d.plugins.tooltip.titleColor = d.plugins.tooltip.bodyColor = cssVar('--color-on-dark');
		d.elements.line.borderWidth = 2;
		d.elements.line.borderCapStyle = 'round';
		d.elements.line.borderJoinStyle = 'round';
		d.elements.point.radius = 0;
		d.elements.point.hoverRadius = 4;
		d.elements.point.hoverBorderWidth = 2;
		d.elements.point.borderColor = canvas;
		d.elements.bar.borderRadius = 4;
		d.elements.arc.borderColor = canvas;
		d.elements.arc.borderWidth = 2;
		d.datasets.bar.maxBarThickness = 24;
	}
</script>

<script lang="ts">
	import { untrack } from 'svelte';

	// Build `config` in a $derived (plain objects, not $state): Chart.js keeps and mutates what it is given.
	// print (the PDF page, FR-P4): drawn at once with no animation, at twice the pixels so the PDF image stays sharp.
	let { config, label, print = false }: { config: ChartConfiguration; label: string; print?: boolean } = $props();

	let canvas: HTMLCanvasElement;
	let chart: Chart | undefined;

	$effect(() => {
		applyTheme();
		const c = new Chart(
			canvas,
			untrack(() => (print ? { ...config, options: { ...config.options, animation: false, devicePixelRatio: 2 } } : config))
		);
		chart = c;
		return () => c.destroy();
	});

	// New data (filters, drill-down): update in place rather than rebuild.
	$effect(() => {
		const { data, options } = config;
		if (!chart || chart.data === data) return;
		chart.data = data;
		chart.options = options ?? {};
		chart.update();
	});
</script>

<!-- The canvas is a picture to assistive tech (Chart.js accessibility docs); its values are in the page's table view. -->
<!-- svelte-ignore a11y_no_interactive_element_to_noninteractive_role -->
<canvas bind:this={canvas} role="img" aria-label={label}></canvas>
