<script>
    // Aircraft search: registration, hex code, callsign, type or operator.
    // Opens directly with ?q=... (the feeder home page links here).
    import { onMount, tick } from 'svelte';
    import { IconSearch, IconDownload, IconMap2, IconPhoto, IconArrowLeft, IconInfoCircle } from '@tabler/icons-svelte';
    import { units } from '../stores/units';
    import { formatDistanceKm, formatAltitude } from '../lib/units';

    let query = '';
    let days = '0';
    let inputEl;

    let loading = false;
    let error = '';
    let results = null; // list of matching aircraft
    let more = false;
    let searchedFor = '';

    let detail = null; // one aircraft and its visits
    let detailLoading = false;
    let visitsMore = false;
    const PAGE = 25;

    let timer;
    let seq = 0;

    function onInput() {
        clearTimeout(timer);
        timer = setTimeout(runSearch, 350);
    }

    async function runSearch() {
        clearTimeout(timer);
        const q = query.trim();
        updateUrl(q);
        if (q.replace(/[^a-z0-9]/gi, '').length < 2) {
            results = null;
            detail = null;
            error = '';
            return;
        }
        const mine = ++seq;
        loading = true;
        error = '';
        try {
            const res = await fetch(`api/search?q=${encodeURIComponent(q)}&days=${days}`);
            const body = await res.json();
            if (mine !== seq) return; // a newer search is running
            if (!res.ok) throw new Error(body.error || res.statusText);
            results = body.results;
            more = body.more;
            searchedFor = q;
            detail = null;
            if (results.length === 1) openAircraft(results[0].hex, false);
        } catch (e) {
            if (mine === seq) error = `Search failed: ${e.message}`;
        } finally {
            if (mine === seq) loading = false;
        }
    }

    async function openAircraft(hex, fromList = true) {
        detailLoading = true;
        error = '';
        try {
            const res = await fetch(`api/search/aircraft/${encodeURIComponent(hex)}?limit=${PAGE}`);
            const body = await res.json();
            if (!res.ok) throw new Error(body.error || res.statusText);
            detail = { ...body, fromList };
            visitsMore = body.visits_list.length === PAGE && body.visits_in_history > PAGE;
            await tick();
            document.getElementById('search-detail')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
        } catch (e) {
            error = `Could not load this aircraft: ${e.message}`;
        } finally {
            detailLoading = false;
        }
    }

    async function loadMoreVisits() {
        const offset = detail.visits_list.length;
        const res = await fetch(`api/search/aircraft/${encodeURIComponent(detail.hex)}?limit=${PAGE}&offset=${offset}`);
        if (!res.ok) return;
        const body = await res.json();
        detail = { ...detail, visits_list: [...detail.visits_list, ...body.visits_list] };
        visitsMore = body.visits_list.length === PAGE && detail.visits_list.length < detail.visits_in_history;
    }

    function updateUrl(q) {
        try {
            const url = new URL(window.location.href);
            if (q) url.searchParams.set('q', q); else url.searchParams.delete('q');
            history.replaceState(null, '', url);
        } catch (e) { /* ignore */ }
    }

    // ---------- formatting ----------
    const sameDay = (a, b) => a.toDateString() === b.toDateString();
    function fmtDay(d) {
        const now = new Date();
        const yesterday = new Date(now);
        yesterday.setDate(now.getDate() - 1);
        if (sameDay(d, now)) return 'Today';
        if (sameDay(d, yesterday)) return 'Yesterday';
        return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: d.getFullYear() === now.getFullYear() ? undefined : 'numeric' });
    }
    const fmtTime = (d) => d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
    function fmtWhen(first, last) {
        const a = new Date(first), b = new Date(last);
        return `${fmtDay(a)} ${fmtTime(a)} to ${sameDay(a, b) ? '' : fmtDay(b) + ' '}${fmtTime(b)}`;
    }
    const fmtDate = (v) => v ? `${fmtDay(new Date(v))} ${fmtTime(new Date(v))}` : '–';
    const fmtShortDate = (v) => v ? fmtDay(new Date(v)) : '–';
    function fmtDuration(first, last) {
        const m = Math.max(1, Math.round((new Date(last) - new Date(first)) / 60000));
        return m < 60 ? `${m} min` : `${Math.floor(m / 60)} h ${m % 60} min`;
    }
    const dist = (km, u) => (km === null || km === undefined) ? '–' : formatDistanceKm(km, u);
    const alt = (ft, u) => (ft === null || ft === undefined || ft <= 0) ? '–' : formatAltitude(ft, u);
    const airport = (iata, icao) => iata || icao || '';
    function route(v) {
        const o = airport(v.origin, v.origin_icao), d = airport(v.destination, v.destination_icao);
        return o && d ? `${o} to ${d}` : '–';
    }
    // SkyStats runs on its own port; the live map is reached through the
    // feeder's main page (which forwards /map/ to tar1090)
    const mapLink = (hex) => `${window.location.protocol}//${window.location.hostname}/map/?icao=${encodeURIComponent(hex)}`;
    const groupBadge = { Mil: 'Military', Gov: 'Government', Pol: 'Police', Civ: 'Civilian' };
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;

    onMount(() => {
        const q = new URLSearchParams(window.location.search).get('q');
        if (q) {
            query = q;
            runSearch();
        }
        const focus = () => setTimeout(() => inputEl?.focus(), 50);
        window.addEventListener('skystats-open-search', focus);
        return () => window.removeEventListener('skystats-open-search', focus);
    });
</script>

<div class="card bg-base-100 shadow-sm rounded mb-6">
    <div class="card-body">
        <div class="flex items-center gap-2 mb-2">
            <IconSearch class="w-6 h-6 text-primary" />
            <h2 class="text-2xl font-extralight tracking-wider">Search your history</h2>
        </div>
        <form class="flex flex-col sm:flex-row gap-2" on:submit|preventDefault={runSearch}>
            <input
                bind:this={inputEl}
                bind:value={query}
                on:input={onInput}
                type="search"
                class="input input-bordered w-full"
                placeholder="N812DN, A1B2C3, DAL1423, B739 or Delta"
                aria-label="Registration, hex code, callsign, type or operator"
                autocomplete="off"
                spellcheck="false" />
            <select class="select select-bordered sm:w-44" bind:value={days} on:change={runSearch} aria-label="Time period">
                <option value="0">All time</option>
                <option value="7">Last 7 days</option>
                <option value="30">Last 30 days</option>
                <option value="365">Last 12 months</option>
            </select>
        </form>
        <p class="text-sm text-base-content/60 mt-1">
            Matches registration, hex code, callsign, aircraft type (for example B739 or C17) or operator name.
            Results appear as you type.
        </p>
    </div>
</div>

{#if error}
    <div class="alert alert-error mb-6"><span>{error}</span></div>
{/if}

{#if loading && !results}
    <div class="flex justify-center py-8"><span class="loading loading-ring loading-lg"></span></div>
{/if}

{#if results && !(detail && !detail.fromList)}
    <div class="card bg-base-100 shadow-sm rounded mb-6">
        <div class="card-body">
            {#if results.length === 0}
                <div class="alert alert-info">
                    <IconInfoCircle class="w-6 h-6" />
                    <span>Nothing matching “{searchedFor}” has been seen{days !== '0' ? ' in this period' : ''}.</span>
                </div>
            {:else}
                <p class="text-sm text-base-content/60 mb-2">
                    {results.length}{more ? '+' : ''} aircraft match “{searchedFor}”{more ? ' (showing the most recent 200)' : ''}. Click one to see its visits.
                </p>
                <div class="overflow-x-auto">
                    <table class="table table-sm">
                        <thead>
                            <tr class="uppercase tracking-wider">
                                <th>Registration</th><th>Operator</th><th>Type</th><th class="text-right">Visits</th><th>Last seen</th>
                            </tr>
                        </thead>
                        <tbody>
                            {#each results as a (a.hex)}
                                <tr class="hover cursor-pointer {detail?.hex === a.hex ? 'bg-base-200' : ''}" on:click={() => openAircraft(a.hex)}>
                                    <td class="font-mono whitespace-nowrap">
                                        <button class="link link-hover link-primary" on:click|stopPropagation={() => openAircraft(a.hex)}>{a.registration || a.hex.toUpperCase()}</button>
                                    </td>
                                    <td>
                                        {a.operator || '–'}
                                        {#if groupBadge[a.interesting_group]}
                                            <span class="badge badge-sm badge-warning ml-1">{groupBadge[a.interesting_group]}</span>
                                        {/if}
                                    </td>
                                    <td class="whitespace-nowrap">{a.model ? `${a.model} (${a.type})` : (a.type || '–')}</td>
                                    <td class="text-right">{a.visits.toLocaleString()}</td>
                                    <td class="whitespace-nowrap">{fmtShortDate(a.last_seen)}</td>
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>
            {/if}
        </div>
    </div>
{/if}

{#if detailLoading}
    <div class="flex justify-center py-8"><span class="loading loading-ring loading-lg"></span></div>
{/if}

{#if detail}
    <div id="search-detail" class="card bg-base-100 shadow-sm rounded mb-6 scroll-mt-20">
        <div class="card-body">
            {#if detail.fromList && results && results.length > 1}
                <button class="btn btn-ghost btn-sm self-start -ml-2 mb-1" on:click={() => (detail = null)}>
                    <IconArrowLeft class="w-4 h-4" /> Back to all {results.length} results
                </button>
            {/if}
            <div class="flex flex-col sm:flex-row gap-4 items-start">
                {#if detail.photo_thumbnail}
                    <a href={detail.photo || detail.photo_thumbnail} target="_blank" rel="noopener noreferrer" class="shrink-0">
                        <img src={detail.photo_thumbnail} alt="Photo of {detail.registration}" class="w-36 h-24 object-cover rounded" loading="lazy" referrerpolicy="no-referrer" />
                    </a>
                {:else}
                    <div class="w-36 h-24 rounded bg-base-200 flex items-center justify-center shrink-0 text-base-content/40">
                        <IconPhoto class="w-8 h-8" />
                    </div>
                {/if}
                <div class="min-w-0">
                    <h3 class="text-2xl font-light">
                        <span class="font-mono">{detail.registration || detail.hex.toUpperCase()}</span>
                        {#if detail.interesting?.operator || detail.owner}
                            <span class="badge badge-primary badge-outline align-middle ml-1">{detail.interesting?.operator || detail.owner}</span>
                        {/if}
                        {#if groupBadge[detail.interesting?.group]}
                            <span class="badge badge-warning align-middle ml-1">{groupBadge[detail.interesting.group]}</span>
                        {/if}
                    </h3>
                    <p class="text-sm text-base-content/70">
                        {[detail.manufacturer, detail.model].filter(Boolean).join(' ')}{detail.type ? ` (${detail.type})` : ''}
                        · hex <span class="font-mono">{detail.hex.toUpperCase()}</span>
                        {#if detail.owner_country}· registered in {detail.owner_country}{/if}
                    </p>
                    <div class="flex flex-wrap gap-3 mt-2 text-sm">
                        <a class="link link-primary inline-flex items-center gap-1" href={mapLink(detail.hex)} target="_blank" rel="noopener">
                            <IconMap2 class="w-4 h-4" /> Open on live map
                        </a>
                        {#if detail.photo}
                            <a class="link link-primary inline-flex items-center gap-1" href={detail.photo} target="_blank" rel="noopener noreferrer">
                                <IconPhoto class="w-4 h-4" /> Photo
                            </a>
                        {/if}
                        {#if detail.interesting?.link}
                            <a class="link link-primary" href={detail.interesting.link} target="_blank" rel="noopener noreferrer">More about this aircraft</a>
                        {/if}
                    </div>
                </div>
            </div>

            <div class="grid grid-cols-2 sm:grid-cols-5 gap-2 my-4">
                <div class="bg-base-200 rounded p-3"><div class="text-xs text-base-content/60">Visits</div><div class="text-lg">{detail.visits.toLocaleString()}</div></div>
                <div class="bg-base-200 rounded p-3"><div class="text-xs text-base-content/60">First seen</div><div class="text-lg">{fmtShortDate(detail.first_seen)}</div></div>
                <div class="bg-base-200 rounded p-3"><div class="text-xs text-base-content/60">Last seen</div><div class="text-lg">{fmtDate(detail.last_seen)}</div></div>
                <div class="bg-base-200 rounded p-3"><div class="text-xs text-base-content/60">Closest</div><div class="text-lg">{dist(detail.closest_km, $units)}</div></div>
                <div class="bg-base-200 rounded p-3"><div class="text-xs text-base-content/60">Highest</div><div class="text-lg">{alt(detail.max_altitude_ft, $units)}</div></div>
            </div>

            {#if detail.visits > detail.visits_in_history}
                <p class="text-sm text-base-content/60 mb-2">
                    {(detail.visits - detail.visits_in_history).toLocaleString()} older visit(s) are counted above, but their details
                    were removed by the history limit.
                </p>
            {/if}

            <div class="overflow-x-auto">
                <table class="table table-sm">
                    <thead>
                        <tr class="uppercase tracking-wider">
                            <th>When</th><th>Callsign</th><th>Route</th><th>Closest</th><th>Max alt</th><th>In view</th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each detail.visits_list as v}
                            <tr>
                                <td class="whitespace-nowrap">{fmtWhen(v.first_seen, v.last_seen)}</td>
                                <td class="font-mono">{v.callsign || '–'}</td>
                                <td class="whitespace-nowrap" title={v.origin_name && v.destination_name ? `${v.origin_name} to ${v.destination_name}` : ''}>{route(v)}</td>
                                <td class="whitespace-nowrap">{dist(v.closest_km, $units)}</td>
                                <td class="whitespace-nowrap">{alt(v.max_altitude_ft, $units)}</td>
                                <td class="whitespace-nowrap">{fmtDuration(v.first_seen, v.last_seen)}</td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            </div>
            <div class="flex flex-wrap gap-3 items-center mt-3 text-sm">
                {#if visitsMore}
                    <button class="btn btn-sm btn-outline" on:click={loadMoreVisits}>Show more visits</button>
                {/if}
                <a class="link link-primary inline-flex items-center gap-1" href="api/search/aircraft/{detail.hex}/visits.csv" download>
                    <IconDownload class="w-4 h-4" /> Download these visits (CSV)
                </a>
                <span class="text-base-content/50">Times are in {tz}.</span>
            </div>
        </div>
    </div>
{/if}
