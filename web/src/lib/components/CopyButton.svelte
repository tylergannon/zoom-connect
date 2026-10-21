<script lang="ts">
	import Copy from '@lucide/svelte/icons/copy';
	import Check from '@lucide/svelte/icons/check';
	import { toast } from 'svelte-sonner';
	import { Button, type ButtonSize, type ButtonVariant } from '#lib/components/ui/button';
	import { copyText } from '#lib/api';

	let {
		text,
		label = 'Copy',
		/** Extra words for screen readers, such as "to Weekly sync". */
		target = '',
		done = 'Copied',
		iconOnly = false,
		variant = 'outline',
		size = 'default',
		class: className = ''
	}: {
		text: string;
		label?: string;
		target?: string;
		done?: string;
		iconOnly?: boolean;
		variant?: ButtonVariant;
		size?: ButtonSize;
		class?: string;
	} = $props();

	let copied = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function copy() {
		if (await copyText(text)) {
			copied = true;
			clearTimeout(timer);
			timer = setTimeout(() => (copied = false), 2000);
			toast.success(done);
		} else {
			toast.error('Couldn’t copy', { description: 'Your browser blocked the clipboard. Select the text and copy it instead.' });
		}
	}
</script>

<Button {variant} {size} class={className} onclick={copy}>
	{#if copied}<Check aria-hidden="true" />{:else}<Copy aria-hidden="true" />{/if}
	{#if iconOnly}
		<span class="sr-only">{label}{target ? ` ${target}` : ''}</span>
	{:else}
		{copied ? 'Copied' : label}{#if target}<span class="sr-only"> {target}</span>{/if}
	{/if}
</Button>
