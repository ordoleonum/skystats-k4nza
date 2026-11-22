<script>
    // Data export panel.
    import { onMount } from 'svelte';
    import { stationTz, stationDate } from '../lib/stationTz.js';
    import { IconDownload, IconFileZip, IconInfoCircle } from '@tabler/icons-svelte';

    let summary = null;
    let loadError = '';
    let fromDate = '';
    let toDate = '';
    const tz = stationTz();

    async function loadSummary() {
        loadError = '';
        try {
            const res = await fetch('api/export/summary');
            if (!res.ok) throw new Error(res.statusText);
            summary = await res.json();
        } catch (e) {
            loadError = 'Could not load export information. Is SkyStats running?';
        }
    }

    function query() {
        const p = new URLSearchParams({ tz });
        if (fromDate) p.set('from', fromDate);
        if (toDate) p.set('to', toDate);
        return p.toString();
    }

    function download(url) {
        // a plain link lets the browser stream straight to disk
        const a = document.createElement('a');
        a.href = url;
        a.download = '';
        document.body.appendChild(a);
        a.click();
        a.remove();
    }

    function setPreset(days) {
        if (days === 0) { fromDate = ''; toDate = ''; return; }
        // the station's calendar days (YYYY-MM-DD)
        fromDate = stationDate(days - 1);
        toDate = stationDate(0);
    }

    const fmtDate = (d) => d ? new Date(d).toLocaleDateString(undefined, { timeZone: tz }) : '—';
    const fmtRows = (n) => n < 0 ? '?' : n.toLocaleString();

    onMount(loadSummary);
</script>

<h4 class="text-lg font-semibold mb-2">Export Your Data</h4>
<p class="text-sm text-base-content/70 mb-6">
    Download what SkyStats has recorded as CSV files. They open directly in Excel, Numbers or Google Sheets.
    Exporting is read-only: nothing is deleted.
</p>

{#if loadError}
    <div class="alert alert-warning mb-4">{loadError}</div>
{/if}

{#if summary}
    <div class="stats stats-vertical lg:stats-horizontal shadow mb-6 w-full">
        <div class="stat">
            <div class="stat-title">Recording since</div>
            <div class="stat-value text-lg">{fmtDate(summary.oldest)}</div>
        </div>
        <div class="stat">
            <div class="stat-title">Latest flight</div>
            <div class="stat-value text-lg">{fmtDate(summary.newest)}</div>
        </div>
        <div class="stat">
            <div class="stat-title">Database size</div>
            <div class="stat-value text-lg">{summary.database_size || '—'}</div>
            <div class="stat-desc">
                {summary.retention_days > 0 ? `Keeps ${summary.retention_days} days` : 'Keeps everything'}
            </div>
        </div>
    </div>
{/if}

<!-- date range -->
<div class="mb-6">
    <p class="text-xl font-extralight tracking-wider mb-2">Date range</p>
    <p class="text-sm text-base-content/70 mb-3">
        Leave both empty to export everything. Dates are in the station's time zone ({tz}).
    </p>
    <div class="flex flex-wrap items-center gap-3 mb-3">
        <label class="flex items-center gap-2">From
            <input type="date" class="input input-sm w-40" bind:value={fromDate} />
        </label>
        <label class="flex items-center gap-2">To
            <input type="date" class="input input-sm w-40" bind:value={toDate} />
        </label>
    </div>
    <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-xs btn-outline" on:click={() => setPreset(1)}>Today</button>
        <button type="button" class="btn btn-xs btn-outline" on:click={() => setPreset(7)}>Last 7 days</button>
        <button type="button" class="btn btn-xs btn-outline" on:click={() => setPreset(30)}>Last 30 days</button>
        <button type="button" class="btn btn-xs btn-outline" on:click={() => setPreset(365)}>Last year</button>
        <button type="button" class="btn btn-xs btn-outline" on:click={() => setPreset(0)}>Everything</button>
    </div>
</div>

<!-- everything -->
<button type="button" class="btn btn-primary mb-6" on:click={() => download('api/export/all.zip?' + query())}
        title="Downloads one ZIP file containing every dataset below as a separate CSV file">
    <IconFileZip class="h-5 w-5" /> Download everything (ZIP)
</button>

<!-- per dataset -->
<p class="text-xl font-extralight tracking-wider mb-2">Or download one dataset</p>
<ul class="list bg-base-200 rounded-box soft-divider">
    {#each (summary ? summary.datasets : []) as ds}
        <li class="list-row items-center">
            <div class="list-col-grow">
                <div class="font-medium">{ds.label}
                    <span class="badge badge-ghost badge-sm ml-2">{fmtRows(ds.rows)} rows</span>
                </div>
                <div class="text-xs text-base-content/60">
                    {ds.description}
                    {#if !ds.has_date_filter}
                        <span class="inline-flex items-center gap-1 ml-1" title="Reference data: always exported in full">
                            <IconInfoCircle class="h-3 w-3" /> date range not applied
                        </span>
                    {/if}
                </div>
            </div>
            <button type="button" class="btn btn-sm btn-outline"
                    on:click={() => download(`api/export/csv/${ds.key}?` + query())}
                    title={'Download ' + ds.label + ' as CSV'}>
                <IconDownload class="h-4 w-4" /> CSV
            </button>
        </li>
    {/each}
</ul>
