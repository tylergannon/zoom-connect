<script lang="ts">
	import { page } from '$app/state';
	import ArtifactViewer from './ArtifactViewer.svelte';
</script>

<ArtifactViewer callId={page.params.id ?? ''} artifactId={page.params.artifactId ?? ''} />
