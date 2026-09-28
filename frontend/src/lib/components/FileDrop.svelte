<script lang="ts" module>
	// Mirrors the server limits (FR-T1). The server re-checks every file by its content.
	export const MAX_FILES = 5;
	export const MAX_IMAGE_BYTES = 10 * 1024 * 1024;
	export const MAX_VIDEO_BYTES = 100 * 1024 * 1024;
	const IMAGE = /\.(jpe?g|png|hei[cf])$/i;
	const VIDEO = /\.(mp4|mov)$/i;

	export type FileProblem = 'type' | 'image_size' | 'video_size' | 'limit';

	/** Returns why a file cannot be added, or null. */
	export function fileProblem(f: File, count: number): FileProblem | null {
		if (count >= MAX_FILES) return 'limit';
		const isImage = f.type.startsWith('image/') || IMAGE.test(f.name);
		const isVideo = f.type.startsWith('video/') || VIDEO.test(f.name);
		if (!isImage && !isVideo) return 'type';
		if (isImage && f.size > MAX_IMAGE_BYTES) return 'image_size';
		if (isVideo && f.size > MAX_VIDEO_BYTES) return 'video_size';
		return null;
	}
</script>

<script lang="ts">
	import { m } from '$lib/paraglide/messages';

	let { files = $bindable([]) }: { files?: File[] } = $props();
	let problems = $state<string[]>([]);
	const uid = $props.id();

	const messages: Record<FileProblem, (name: string) => string> = {
		type: (name) => m.err_file_type({ name }),
		image_size: (name) => m.err_file_image_size({ name }),
		video_size: (name) => m.err_file_video_size({ name }),
		limit: () => m.err_file_limit()
	};

	function add(list: FileList | null) {
		const next = [...files];
		const found: string[] = [];
		for (const f of list ?? []) {
			const p = fileProblem(f, next.length);
			if (p) found.push(messages[p](f.name));
			else next.push(f);
		}
		files = next;
		problems = found;
	}

	function remove(i: number) {
		files = files.filter((_, j) => j !== i);
		problems = [];
	}

	const size = (b: number) => `${(b / 1024 / 1024).toFixed(1)} MB`;
	// Object URLs for image previews, released when the file list changes.
	const previews = $derived(files.map((f) => (f.type.startsWith('image/') ? URL.createObjectURL(f) : '')));
	$effect(() => () => previews.forEach((u) => u && URL.revokeObjectURL(u)));
</script>

<div class="field">
	<!-- No `capture` attribute: phones still offer the camera, and guests can also pick from the gallery. -->
	<!-- Visually hidden: the native button shows the browser's language, not the guest's. The label opens it. -->
	<input
		id="files-{uid}"
		class="visually-hidden"
		type="file"
		multiple
		accept="image/jpeg,image/png,image/heic,image/heif,video/mp4,video/quicktime"
		aria-describedby={problems.length ? `files-${uid}-error` : undefined}
		onchange={(e) => {
			add(e.currentTarget.files);
			e.currentTarget.value = '';
		}}
	/>
	<label for="files-{uid}" class="drop">
		<span class="plus" aria-hidden="true">+</span>
		<span class="title">{m.form_evidence()}</span>
		<span class="helper">{m.form_evidence_helper()}</span>
	</label>
	{#if problems.length}
		<ul id="files-{uid}-error" class="error" aria-live="polite">
			{#each problems as p (p)}<li>{p}</li>{/each}
		</ul>
	{/if}
	{#if files.length}
		<ul class="list">
			{#each files as f, i (f)}
				<li>
					{#if previews[i]}<img src={previews[i]} alt="" />{:else}<span class="thumb" aria-hidden="true">▶</span>{/if}
					<span class="name">{f.name}</span>
					<span class="size">{size(f.size)}</span>
					<button type="button" aria-label={m.form_remove_file({ name: f.name })} onclick={() => remove(i)}>✕</button>
				</li>
			{/each}
		</ul>
	{/if}
</div>

<style>
	.field {
		display: flex;
		flex-direction: column;
		gap: var(--space-xs);
	}
	.drop {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-xxs);
		padding: var(--space-lg);
		border: 1px dashed var(--color-muted-soft);
		border-radius: var(--radius-lg);
		background: var(--color-surface-soft);
		text-align: center;
		cursor: pointer;
	}
	.visually-hidden:focus-visible + .drop {
		outline: 2px solid var(--color-focus);
		outline-offset: 2px;
	}
	.plus {
		display: grid;
		place-items: center;
		width: var(--size-icon-button);
		height: var(--size-icon-button);
		margin-bottom: var(--space-xs);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-full);
		background: var(--color-canvas);
		box-shadow: var(--shadow-sm);
		font: var(--font-title-md);
		line-height: 1;
		color: var(--color-ink);
	}
	.title {
		font: var(--font-title-sm);
		color: var(--color-ink);
	}
	.helper {
		font: var(--font-body-sm);
		color: var(--color-muted);
	}
	.list,
	.error {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: var(--space-xs);
	}
	.error {
		font: var(--font-body-sm);
		color: var(--color-error-strong);
	}
	.list li {
		display: flex;
		align-items: center;
		gap: var(--space-sm);
		padding: var(--space-xs);
		border: 1px solid var(--color-hairline);
		border-radius: var(--radius-md);
	}
	img,
	.thumb {
		width: var(--size-icon-button);
		height: var(--size-icon-button);
		border-radius: var(--radius-sm);
		object-fit: cover;
		background: var(--color-surface-card);
		display: grid;
		place-items: center;
		color: var(--color-muted);
	}
	.name {
		flex: 1;
		overflow-wrap: anywhere;
		font: var(--font-body-sm);
		color: var(--color-ink);
	}
	.size {
		font: var(--font-caption);
		color: var(--color-muted);
	}
	button {
		width: var(--size-control);
		height: var(--size-control);
		border: none;
		border-radius: var(--radius-full);
		background: transparent;
		color: var(--color-body);
		cursor: pointer;
	}
</style>
