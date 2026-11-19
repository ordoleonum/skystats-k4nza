<script>
    import { onMount, onDestroy } from 'svelte';

    let data = {};
    let loading = true;
    let error = null;

    async function fetchData() {
        loading = true;
        error = null;
        try {
            const res = await fetch('api/version');
            if (!res.ok) {
                throw new Error(`Error fetching data: ${res.status} ${res.statusText}`);
            }
            data = await res.json();
        } catch (err) {
            error = err.message;
        } finally {
            loading = false;
        }
    }
    onMount(() => {
        fetchData();
    })

    onDestroy(() => {
    });
</script>

<footer class="footer footer-center p-4 bg-base-200 text-base-content rounded">
    {#if loading}
        <div class="text-sm">Loading...</div>
    {:else if error}
        <div class="text-sm text-error">Error: {error}</div>
    {:else if data.version === "dev"}
        <div class="text-sm">{data.version} • {data.commit} • {data.date.toLocaleString()}</div>
    {:else}
        <div class="text-sm">
            <p><a href="https://github.com/ordoleonum/skystats-k4nza" target="_blank" rel="noopener noreferrer" class="hover:text-primary transition-colors">SkyStats-K4NZA</a> • {data.version}</p>
        </div>
    {/if}
</footer>