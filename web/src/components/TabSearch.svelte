<script>
    // Aircraft search: registration, hex code, callsign, type or operator.
    // Opens directly with ?q=... (the feeder home page links here).
    import { onMount, tick } from 'svelte';
    import { IconSearch, IconDownload, IconExternalLink, IconX, IconInfoCircle, IconPlane, IconHelicopter } from '@tabler/icons-svelte';
    import { units } from '../stores/units';
    import { formatDistanceKm, formatAltitude } from '../lib/units';
    import { replayUrl } from '../lib/feeder.js';
    import { wikiTypePhoto } from '../lib/photos.js';

    let query = '';
    let days = '0';
    let group = ''; // listing a whole interesting group (Mil, Gov, Pol, Civ) instead of a text search
    let limit = 200;
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
        group = ''; // typing starts a normal search again
        clearTimeout(timer);
        timer = setTimeout(runSearch, 350);
    }

    function clearGroup() {
        group = '';
        results = null;
        detail = null;
        updateUrl('');
        inputEl?.focus();
    }

    async function runSearch() {
        clearTimeout(timer);
        if (group) return runGroup();
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
            limit = body.limit || 200;
            searchedFor = q;
            detail = null;
            if (results.length === 1) openAircraft(results[0].hex, 'direct');
        } catch (e) {
            if (mine === seq) error = `Search failed: ${e.message}`;
        } finally {
            if (mine === seq) loading = false;
        }
    }

    // every aircraft of one interesting group ever seen
    async function runGroup() {
        const g = group;
        query = '';
        updateUrl('', g);
        const mine = ++seq;
        loading = true;
        error = '';
        try {
            const res = await fetch(`api/search?group=${encodeURIComponent(g)}&days=${days}`);
            const body = await res.json();
            if (mine !== seq) return;
            if (!res.ok) throw new Error(body.error || res.statusText);
            results = body.results;
            more = body.more;
            limit = body.limit || 1000;
            searchedFor = groupName[g] || g;
            detail = null;
        } catch (e) {
            if (mine === seq) error = `Search failed: ${e.message}`;
        } finally {
            if (mine === seq) loading = false;
        }
    }

    // ---------- sorting the results (click a column header) ----------
    let sortKey = ''; // '' = as the server sent them (most recently seen first)
    let sortDir = 1;
    const sortValue = {
        reg: (a) => (a.registration || a.hex).toUpperCase(),
        operator: (a) => (a.operator || '').toUpperCase(),
        type: (a) => (a.model ? `${a.model} (${a.type})` : (a.type || '')).toUpperCase(),
        visits: (a) => a.visits,
        last: (a) => new Date(a.last_seen).getTime() || 0
    };
    function sortBy(key) {
        if (sortKey === key) {
            sortDir = -sortDir;
        } else {
            sortKey = key;
            // numbers and dates: biggest / newest first; text: A to Z
            sortDir = key === 'visits' || key === 'last' ? -1 : 1;
        }
    }
    function sorted(list, key, dir) {
        if (!list || !key) return list || [];
        const v = sortValue[key];
        return [...list].sort((a, b) => {
            const x = v(a), y = v(b);
            // empty text always last
            if (x === '' && y !== '') return 1;
            if (y === '' && x !== '') return -1;
            return (x < y ? -1 : x > y ? 1 : 0) * dir;
        });
    }
    $: shown = sorted(results, sortKey, sortDir);

    // the list starts collapsed to the first few rows; "See more" shows them all
    const COLLAPSED = 10;
    let expanded = false;
    $: results, (expanded = false); // a new search collapses it again
    $: rows = expanded ? shown : shown.slice(0, COLLAPSED);
    const arrow = (key, k, d) => (k === key ? (d > 0 ? ' ▲' : ' ▼') : '');

    // where the details open: 'list' (under a result row), 'recent' (under a row
    // of the recently seen list, before any search) or 'direct' (a search with a
    // single match: shown on its own)
    let loadingHex = ''; // the row whose details are loading (shown under it)
    async function openAircraft(hex, from = 'list') {
        if (from !== 'direct' && detail?.hex === hex && detail.from === from) {
            detail = null; // a second click on the open row closes it
            return;
        }
        loadingHex = from !== 'direct' ? hex : '';
        detailLoading = true;
        error = '';
        try {
            const res = await fetch(`api/search/aircraft/${encodeURIComponent(hex)}?limit=${PAGE}`);
            const body = await res.json();
            if (!res.ok) throw new Error(body.error || res.statusText);
            detail = { ...body, from };
            loadPhoto(body);
            visitsMore = body.visits_list.length === PAGE && body.visits_in_history > PAGE;
            await tick();
            document.getElementById('search-detail')?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        } catch (e) {
            error = `Could not load this aircraft: ${e.message}`;
        } finally {
            detailLoading = false;
            loadingHex = '';
        }
    }

    // Photo from planespotters.net (as on the aircraft card at the top of
    // SkyStats), credited to the photographer; the registration lookup's photo
    // is the fallback, then a photo of the aircraft TYPE from Wikipedia (clearly
    // labelled as not this aircraft).
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
        if (!found) found = await typePhoto(d);
        if (detail?.hex === hex) photo = found; // still the same aircraft
    }

    // Wikipedia's photo of the aircraft TYPE (lib/photos.js): the full name
    // first (registration lookup, plane-alert-db, ICAO type list), then the code
    function typePhoto(d) {
        const model = (d.model || '').trim();
        const maker = (d.manufacturer || '').trim();
        const names = [];
        if (model) names.push(maker && !model.toLowerCase().includes(maker.toLowerCase().split(' ')[0]) ? `${maker} ${model}` : model);
        names.push(d.interesting?.type, d.type_name, d.type);
        // the label names the type in words ("Typical Boeing 737-800"), not its code
        return wikiTypePhoto(names, d.type_name || d.interesting?.type || model || d.type);
    }

    async function loadMoreVisits() {
        const offset = detail.visits_list.length;
        const res = await fetch(`api/search/aircraft/${encodeURIComponent(detail.hex)}?limit=${PAGE}&offset=${offset}`);
        if (!res.ok) return;
        const body = await res.json();
        detail = { ...detail, visits_list: [...detail.visits_list, ...body.visits_list] };
        visitsMore = body.visits_list.length === PAGE && detail.visits_list.length < detail.visits_in_history;
    }

    function updateUrl(q, g = '') {
        try {
            const url = new URL(window.location.href);
            if (q) url.searchParams.set('q', q); else url.searchParams.delete('q');
            if (g) url.searchParams.set('group', g); else url.searchParams.delete('group');
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
    // ADSBExchange's map, by hex code (it shows the aircraft live when it's
    // flying, and its history from there)
    const adsbxLink = (d) => `https://globe.adsbexchange.com/?icao=${encodeURIComponent(d.hex.toLowerCase())}`;
    const groupBadge = { Mil: 'Military', Gov: 'Government', Pol: 'Police', Civ: 'Civilian' };
    const FEEDER_MILITARY = 'Military (feeder database)';
    // no photo: a plane or helicopter outline with the type, instead of an empty box
    const HELI = /^(H\d|EC\d|AS\d{2}|A1[0-9]{2}|B06|B4\d\d|B505|R22|R44|R66|S76|S92|UH|AH|CH|MH|EN48|BK17|MI8|NH90)/;
    const isHelicopter = (d) => HELI.test((d.type || '').toUpperCase()) || /helicopter|copter/i.test((d.model || '') + ' ' + (d.manufacturer || ''));

    // ---------- before searching: group shortcuts and recent interesting aircraft ----------
    const groupKeys = ['Mil', 'Gov', 'Pol', 'Civ'];
    const groupEndpoint = { Mil: 'military', Gov: 'government', Pol: 'police', Civ: 'civilian' };
    let recentInteresting = null;
    // the latest 100 of each group; 10 rows shown until "See more"
    const RECENT_FETCH = 100;
    let recentExpanded = false;
    $: recentRows = recentInteresting ? (recentExpanded ? recentInteresting : recentInteresting.slice(0, COLLAPSED)) : [];
    async function loadRecentInteresting() {
        try {
            const lists = await Promise.all(groupKeys.map(async (g) => {
                const res = await fetch(`api/stats/interesting/${groupEndpoint[g]}?limit=${RECENT_FETCH}`);
                if (!res.ok) return [];
                return (await res.json()).map((a) => ({ ...a, group: a.group || g }));
            }));
            recentInteresting = lists.flat()
                .filter((a) => a.hex)
                .sort((a, b) => new Date(b.seen) - new Date(a.seen))
                .filter((a, i, all) => all.findIndex((x) => x.hex === a.hex) === i); // once per aircraft
        } catch (e) {
            recentInteresting = [];
        }
    }
    function pickGroup(g) {
        group = g;
        days = '0';
        runSearch();
    }
    const groupName = { Mil: 'Military aircraft', Gov: 'Government aircraft', Pol: 'Police aircraft', Civ: 'Civilian aircraft of interest' };
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;

    onMount(() => {
        const params = new URLSearchParams(window.location.search);
        const q = params.get('q');
        const g = params.get('group');
        if (g && groupName[g]) {
            group = g;
            runSearch();
        } else if (q) {
            query = q;
            runSearch();
        }
        loadRecentInteresting();
        const focus = () => setTimeout(() => inputEl?.focus(), 50);
        window.addEventListener('skystats-open-search', focus);
        // a registration clicked in Interesting Aircraft / Record Holders:
        // search the whole history for it
        // or a group card header: list the whole group
        const searchFor = (e) => {
            days = '0';
            if (e.detail && e.detail.group) {
                group = e.detail.group;
            } else {
                group = '';
                query = e.detail;
            }
            runSearch();
        };
        window.addEventListener('skystats-search-for', searchFor);
        return () => {
            window.removeEventListener('skystats-open-search', focus);
            window.removeEventListener('skystats-search-for', searchFor);
        };
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
        {#if group}
            <div class="flex flex-wrap items-center gap-2 mt-1">
                <span class="badge grp-badge grp-{group}">{groupName[group]}</span>
                <span class="text-sm text-base-content/60">Every aircraft of this group seen{days !== '0' ? ' in this period' : ''}.</span>
                <button type="button" class="btn btn-ghost btn-xs" on:click={clearGroup}>Clear</button>
            </div>
        {:else}
            <p class="text-sm text-base-content/60 mt-1">
                Matches registration, hex code, callsign, aircraft type (for example B739 or C17) or operator name.
                Results appear as you type.
            </p>
        {/if}
    </div>
</div>

{#if error}
    <div class="alert alert-error mb-6"><span>{error}</span></div>
{/if}

{#if !group && !results && !loading && !error && !(detail && detail.from !== 'recent')}
    <div class="card bg-base-100 shadow-sm rounded mb-6">
        <div class="card-body">
            <div class="flex flex-wrap items-center gap-2">
                <span class="text-sm text-base-content/60 mr-1">Show every aircraft seen in a group:</span>
                {#each groupKeys as g}
                    <button type="button" class="btn btn-sm btn-outline grp-btn-{g}" on:click={() => pickGroup(g)}>{groupBadge[g]}</button>
                {/each}
            </div>
            {#if recentInteresting && recentInteresting.length}
                <h3 class="text-lg font-light mt-4 mb-1">Recently seen interesting aircraft</h3>
                <div class="overflow-x-auto">
                    <table class="table table-sm">
                        <tbody>
                            {#each recentRows as a (a.hex)}
                                <tr class="hover cursor-pointer {detail?.hex === a.hex ? 'bg-base-200' : ''}" on:click={() => openAircraft(a.hex, 'recent')}>
                                    <td class="font-mono whitespace-nowrap">
                                        <button class="link link-hover link-primary" on:click|stopPropagation={() => openAircraft(a.hex, 'recent')}>{a.registration || a.hex.toUpperCase()}</button>
                                        <div class="sm:hidden text-xs text-base-content/60 font-sans">{a.operator || ''}</div>
                                    </td>
                                    <td class="hidden sm:table-cell">{a.operator || '–'}</td>
                                    <td class="whitespace-nowrap">{a.icao_type || a.type || '–'}
                                        {#if groupBadge[a.group]}<span class="badge badge-sm grp-badge grp-{a.group} ml-1">{groupBadge[a.group]}</span>{/if}</td>
                                    <td class="whitespace-nowrap text-right">{fmtDate(a.seen)}</td>
                                </tr>
                                {#if detailLoading && loadingHex === a.hex}
                                    <tr><td colspan="4"><div class="flex justify-center py-4"><span class="loading loading-ring loading-md"></span></div></td></tr>
                                {:else if detail && detail.from === 'recent' && detail.hex === a.hex}
                                    <tr class="search-detail-row"><td colspan="4" class="px-0 pt-1 pb-3 text-base whitespace-normal">{@render detailCard()}</td></tr>
                                {/if}
                            {/each}
                        </tbody>
                    </table>
                </div>
                {#if recentInteresting.length > COLLAPSED}
                    <div class="flex justify-center mt-2">
                        <button type="button" class="btn btn-sm btn-outline" on:click={() => (recentExpanded = !recentExpanded)} aria-expanded={recentExpanded}>
                            {recentExpanded ? 'See less' : `See more (${(recentInteresting.length - COLLAPSED).toLocaleString()} more)`}
                        </button>
                    </div>
                {/if}
            {/if}
        </div>
    </div>
{/if}

{#if loading && !results}
    <div class="flex justify-center py-8"><span class="loading loading-ring loading-lg"></span></div>
{/if}

{#if detailLoading && !loadingHex}
    <div class="flex justify-center py-8"><span class="loading loading-ring loading-lg"></span></div>
{/if}

{#snippet detailCard()}
    <div id="search-detail" class="card bg-base-100 shadow-sm rounded mb-6 scroll-mt-20">
        <div class="card-body">
            {#if detail.from !== 'direct'}
                <button class="btn btn-ghost btn-sm self-start -ml-2 mb-1" on:click={() => (detail = null)}>
                    <IconX class="w-4 h-4" /> Close
                </button>
            {/if}
            <div class="flex flex-col sm:flex-row gap-4 items-start">
                {#if photo}
                    <a href={photo.link} target="_blank" rel="noopener noreferrer" class="relative shrink-0 block" title={photo.typical ? 'A photo of this type of aircraft, from Wikipedia' : 'Open the photo'}>
                        <img src={photo.src} alt={photo.typical ? `Typical ${photo.typical}` : `Photo of ${detail.registration || detail.hex.toUpperCase()}`} class="w-48 h-32 object-cover rounded" loading="lazy" referrerpolicy="no-referrer" />
                        {#if photo.typical}
                            <span class="absolute top-1 left-1 bg-black/60 text-white text-[10px] px-1.5 py-0.5 rounded">Typical {photo.typical} – not this aircraft</span>
                        {/if}
                        {#if photo.photographer}
                            <span class="absolute bottom-1 right-1 bg-black/60 text-white text-[10px] px-1.5 py-0.5 rounded">© {photo.photographer}</span>
                        {/if}
                    </a>
                {:else}
                    <div class="w-48 h-32 rounded bg-base-200 flex flex-col items-center justify-center shrink-0 text-base-content/50 text-xs gap-1" title="No photo of this aircraft yet">
                        {#if isHelicopter(detail)}<IconHelicopter class="w-14 h-14" stroke={1.2} />{:else}<IconPlane class="w-14 h-14" stroke={1.2} />{/if}
                        <span class="font-mono text-sm">{detail.type || 'Type unknown'}</span>
                        <span>No photo yet</span>
                    </div>
                {/if}
                <div class="min-w-0">
                    <h3 class="text-2xl font-light">
                        <span class="font-mono">{detail.registration || detail.hex.toUpperCase()}</span>
                        {#if detail.interesting?.operator || detail.owner}
                            <span class="badge badge-primary badge-outline align-middle ml-1">{detail.interesting?.operator || detail.owner}</span>
                        {/if}
                        {#if groupBadge[detail.interesting?.group]}
                            <span class="badge grp-badge grp-{detail.interesting.group} align-middle ml-1">{groupBadge[detail.interesting.group]}</span>
                        {/if}
                    </h3>
                    {#if detail.interesting?.category === FEEDER_MILITARY}
                        <p class="text-xs text-base-content/60">Marked military by the feeder's aircraft database (it isn't on the plane-alert list{detail.interesting.operator ? '' : ', and the database has no operator for it'}).</p>
                    {/if}
                    <p class="text-sm text-base-content/70">
                        {[detail.manufacturer, detail.model].filter(Boolean).join(' ') || detail.interesting?.type || detail.type_name || ''}{detail.type ? ` (${detail.type})` : ''}
                        · hex <span class="font-mono">{detail.hex.toUpperCase()}</span>
                        {#if detail.owner_country}· registered in {detail.owner_country}{/if}
                    </p>
                    <div class="flex flex-wrap gap-3 mt-2 text-sm">
                        <a class="link link-primary inline-flex items-center gap-1" href={adsbxLink(detail)} target="_blank" rel="noopener noreferrer">
                            <IconExternalLink class="w-4 h-4" /> View on ADSBExchange
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
                                <td class="whitespace-nowrap">
                                    {#if replayUrl(detail.hex, v.first_seen)}
                                        <a class="link link-primary" href={replayUrl(detail.hex, v.first_seen)} target="_blank" rel="noopener"
                                            title="Replay this visit on the live map">{fmtWhen(v.first_seen, v.last_seen)}</a>
                                    {:else}{fmtWhen(v.first_seen, v.last_seen)}{/if}
                                </td>
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
                <span class="text-base-content/50">Times are in {tz}. Click one to replay that visit on the live map.</span>
            </div>
        </div>
    </div>
{/snippet}

{#if detail && detail.from === 'direct'}
    {@render detailCard()}
{/if}

{#if results && !(detail && detail.from === 'direct')}
    <div class="card bg-base-100 shadow-sm rounded mb-6">
        <div class="card-body">
            {#if results.length === 0}
                <div class="alert alert-info">
                    <IconInfoCircle class="w-6 h-6" />
                    <span>{group ? `No ${searchedFor.toLowerCase()} have been seen` : `Nothing matching “${searchedFor}” has been seen`}{days !== '0' ? ' in this period' : ''}.</span>
                </div>
            {:else}
                <p class="text-sm text-base-content/60 mb-2">
                    {results.length}{more ? '+' : ''} {group ? searchedFor.toLowerCase() : `aircraft match “${searchedFor}”`}{more ? ` (showing the most recent ${limit})` : ''}.
                    Click one to see its visits, or a column heading to sort.
                </p>
                <div class="overflow-x-auto">
                    <table class="table table-sm">
                        <thead>
                            <tr class="uppercase tracking-wider">
                                <th aria-sort={sortKey === 'reg' ? (sortDir > 0 ? 'ascending' : 'descending') : 'none'}><button type="button" class="uppercase tracking-wider hover:text-primary" on:click={() => sortBy('reg')}>Registration{arrow('reg', sortKey, sortDir)}</button></th>
                                <th class="hidden sm:table-cell" aria-sort={sortKey === 'operator' ? (sortDir > 0 ? 'ascending' : 'descending') : 'none'}><button type="button" class="uppercase tracking-wider hover:text-primary" on:click={() => sortBy('operator')}>Operator{arrow('operator', sortKey, sortDir)}</button></th>
                                <th aria-sort={sortKey === 'type' ? (sortDir > 0 ? 'ascending' : 'descending') : 'none'}><button type="button" class="uppercase tracking-wider hover:text-primary" on:click={() => sortBy('type')}>Type{arrow('type', sortKey, sortDir)}</button></th>
                                <th class="text-right hidden sm:table-cell" aria-sort={sortKey === 'visits' ? (sortDir > 0 ? 'ascending' : 'descending') : 'none'}><button type="button" class="uppercase tracking-wider hover:text-primary" on:click={() => sortBy('visits')}>Visits{arrow('visits', sortKey, sortDir)}</button></th>
                                <th aria-sort={sortKey === 'last' ? (sortDir > 0 ? 'ascending' : 'descending') : 'none'}><button type="button" class="uppercase tracking-wider hover:text-primary" on:click={() => sortBy('last')}>Last seen{arrow('last', sortKey, sortDir)}</button></th>
                            </tr>
                        </thead>
                        <tbody>
                            {#each rows as a (a.hex)}
                                <tr class="hover cursor-pointer {detail?.hex === a.hex ? 'bg-base-200' : ''}" on:click={() => openAircraft(a.hex)}>
                                    <td class="font-mono whitespace-nowrap">
                                        <button class="link link-hover link-primary" on:click|stopPropagation={() => openAircraft(a.hex)}>{a.registration || a.hex.toUpperCase()}</button>
                                        <div class="sm:hidden text-xs text-base-content/60 font-sans whitespace-normal">
                                            {a.operator || ''}
                                            {#if groupBadge[a.interesting_group]}<span class="badge badge-xs grp-badge grp-{a.interesting_group} ml-1">{groupBadge[a.interesting_group]}</span>{/if}
                                        </div>
                                    </td>
                                    <td class="hidden sm:table-cell">
                                        {a.operator || '–'}
                                        {#if groupBadge[a.interesting_group]}
                                            <span class="badge badge-sm grp-badge grp-{a.interesting_group} ml-1">{groupBadge[a.interesting_group]}</span>
                                        {/if}
                                    </td>
                                    <td class="whitespace-nowrap"><span class="hidden sm:inline">{a.model ? `${a.model} (${a.type})` : (a.type || '–')}</span><span class="sm:hidden font-mono" title={a.model || ''}>{a.type || '–'}</span></td>
                                    <td class="text-right hidden sm:table-cell">{a.visits.toLocaleString()}</td>
                                    <td class="whitespace-nowrap">{fmtDate(a.last_seen)}</td>
                                </tr>
                                {#if detailLoading && loadingHex === a.hex}
                                    <tr><td colspan="5"><div class="flex justify-center py-4"><span class="loading loading-ring loading-md"></span></div></td></tr>
                                {:else if detail && detail.from === 'list' && detail.hex === a.hex}
                                    <tr class="search-detail-row"><td colspan="5" class="px-0 pt-1 pb-3 text-base whitespace-normal">{@render detailCard()}</td></tr>
                                {/if}
                            {/each}
                        </tbody>
                    </table>
                </div>
                {#if shown.length > COLLAPSED}
                    <div class="flex justify-center mt-2">
                        <button type="button" class="btn btn-sm btn-outline" on:click={() => (expanded = !expanded)} aria-expanded={expanded}>
                            {expanded ? 'See less' : `See more (${(shown.length - COLLAPSED).toLocaleString()} more)`}
                        </button>
                    </div>
                {/if}
            {/if}
        </div>
    </div>
{/if}
