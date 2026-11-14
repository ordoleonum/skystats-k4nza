<script>
    // Aircraft search: registration, hex code, callsign, type or operator.
    // Opens directly with ?q=... (the feeder home page links here).
    import { onMount, tick } from 'svelte';
    import { IconSearch, IconDownload, IconExternalLink, IconPhoto, IconArrowLeft, IconInfoCircle } from '@tabler/icons-svelte';
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
    let photo = null; // { src, link, photographer } for the open aircraft
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
            loadPhoto(body);
            visitsMore = body.visits_list.length === PAGE && body.visits_in_history > PAGE;
            await tick();
            document.getElementById('search-detail')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
        } catch (e) {
            error = `Could not load this aircraft: ${e.message}`;
        } finally {
            detailLoading = false;
        }
    }

    // Photo from planespotters.net (as on the aircraft card at the top of
    // SkyStats), credited to the photographer; the registration lookup's photo
    // is the fallback.
    async function loadPhoto(d) {
        photo = null;
        const hex = d.hex;
        let found = null;
        try {
            const res = await fetch(`https://api.planespotters.net/pub/photos/hex/${encodeURIComponent(hex)}`);
            if (res.ok) {
                const p = (await res.json()).photos?.[0];
                const src = p?.thumbnail_large?.src || p?.thumbnail?.src;
                if (src) found = { src, link: p.link, photographer: p.photographer };
            }
        } catch (e) { /* offline or blocked: try the fallback */ }
        if (!found && d.photo_thumbnail) {
            found = { src: d.photo_thumbnail, link: d.photo || d.photo_thumbnail, photographer: '' };
        }
        if (detail?.hex === hex) photo = found; // still the same aircraft
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
    // FlightAware, the same link the live map uses for a registration (the
    // aircraft may not be in range now); by hex code when there's no registration
    function flightAwareLink(d) {
        const reg = (d.registration || '').trim();
        if (reg) return `https://flightaware.com/live/flight/${encodeURIComponent(reg)}`;
        return `https://flightaware.com/live/modes/${encodeURIComponent(d.hex.replace('~', ''))}/redirect`;
    }
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
                {#if photo}
                    <a href={photo.link} target="_blank" rel="noopener noreferrer" class="relative shrink-0 block" title="Open the photo">
                        <img src={photo.src} alt="Photo of {detail.registration || detail.hex.toUpperCase()}" class="w-48 h-32 object-cover rounded" loading="lazy" referrerpolicy="no-referrer" />
                        {#if photo.photographer}
                            <span class="absolute bottom-1 right-1 bg-black/60 text-white text-[10px] px-1.5 py-0.5 rounded">© {photo.photographer}</span>
                        {/if}
                    </a>
                {:else}
                    <div class="w-48 h-32 rounded bg-base-200 flex flex-col items-center justify-center shrink-0 text-base-content/40 text-xs gap-1">
                        <IconPhoto class="w-8 h-8" />No photo available
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
                        <a class="link link-primary inline-flex items-center gap-1" href={flightAwareLink(detail)} target="_blank" rel="noopener noreferrer">
                            <IconExternalLink class="w-4 h-4" /> View on FlightAware
                        </a>
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
