<script lang="ts">
 import { untrack } from 'svelte';
 import { createActressStore } from '../../src/routes/actresses/stores/actress-store.svelte';
 let { capture }: { capture: (store: ReturnType<typeof createActressStore>) => void } = $props();
 const store = createActressStore();
 untrack(() => capture(store));
</script>
<p>{store.total}</p>
