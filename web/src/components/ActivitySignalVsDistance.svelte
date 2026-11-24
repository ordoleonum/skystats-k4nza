<script>
// @ts-nocheck
    // Signal strength (RSSI) against distance, with the median per 10 nm
    // (distances are converted to the chosen units for display).
    import { onMount, onDestroy } from 'svelte';
    import { themeChanged } from '../lib/themeStore.js';
    import { units } from '../stores/units';
    import { fromNm, round, distanceLabel } from '../lib/units';
    import {
        Chart,
        ScatterController,
        LineController,
        LinearScale,
        PointElement,
        LineElement,
        Tooltip
    } from 'chart.js';

    Chart.register(ScatterController, LineController, LinearScale, PointElement, LineElement, Tooltip);

    // readsb's RSSI tops out at 0 dBFS; anything above about -3 is a sign of
    // too much gain (strong nearby aircraft overloading the receiver).
    const OVERLOAD_DB = -3;

    const periods = [
        { key: 'day', label: '24hrs' },
        { key: 'week', label: 'Week' },
        { key: 'month', label: 'Month' }
    ];
    // the period comes from the Activity tab's switch
    export let period = 'day';
    export let note = '';
    let selected = period;
    $: if (period !== selected) { selected = period; load(); }

    let canvas = null;
    let chart = null;
    let loading = true;
    let error = null;
    let data = null;
    let unsubscribe;

    // Theme colours can come back as oklch(); paint one pixel to get plain RGB.
    function daisyColor(className) {
        const el = document.createElement('div');
        el.className = className;
        document.body.appendChild(el);
        const c = getComputedStyle(el).backgroundColor || getComputedStyle(el).color;
        document.body.removeChild(el);
        const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
        ctx.fillStyle = c;
        ctx.fillRect(0, 0, 1, 1);
        const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
        return `rgb(${r}, ${g}, ${b})`;
    }

    function withAlpha(rgb, a) {
        const m = rgb.match(/[\d.]+/g);
        return m ? `rgba(${m[0]}, ${m[1]}, ${m[2]}, ${a})` : rgb;
    }

    // quiet: the once-a-minute refresh, which keeps the chart on screen (and
    // the last numbers if SkyStats doesn't answer) instead of a loading spinner
    async function load(quiet = false) {
        if (!quiet) loading = true;
        error = null;
        try {
            const res = await fetch(`api/stats/reception/signal/${selected}`);
            if (!res.ok) throw new Error(`HTTP ${res.status}`);
            data = await res.json();
        } catch (e) {
            if (!quiet) {
                error = e.message || 'Failed to load data';
                data = null;
            }
        } finally {
            loading = false;
        }
    }

    function hasData() {
        return data && data.points && data.points.length > 0;
    }

    function draw() {
        if (!canvas || !hasData()) return;
        if (canvas.offsetWidth === 0) {
            requestAnimationFrame(draw);
            return;
        }
        const primary = daisyColor('bg-primary');
        const warning = daisyColor('bg-warning');
        const text = daisyColor('bg-base-content');
        const grid = withAlpha(text, 0.12);

        const u = distanceLabel[$units];
        const dx = (nm) => round(fromNm(nm, $units), 1);
        const maxX = Math.max(50, Math.ceil(Math.max(...data.points.map(p => dx(p[0]))) / 25) * 25);

        if (chart) chart.destroy();
        chart = new Chart(canvas, {
            type: 'scatter',
            data: {
                datasets: [
                    {
                        label: 'Aircraft',
                        data: data.points.map(p => ({ x: dx(p[0]), y: p[1] })),
                        backgroundColor: withAlpha(primary, 0.45),
                        pointRadius: 2.5,
                        pointHoverRadius: 4,
                        order: 3
                    },
                    {
                        type: 'line',
                        label: 'Median',
                        data: data.median.map(p => ({ x: dx(p[0]), y: p[1] })),
                        borderColor: text,
                        borderWidth: 2,
                        pointRadius: 0,
                        pointHitRadius: 6,
                        tension: 0.3,
                        order: 2
                    },
                    {
                        type: 'line',
                        label: 'Overload',
                        data: [{ x: 0, y: OVERLOAD_DB }, { x: maxX, y: OVERLOAD_DB }],
                        borderColor: warning,
                        borderDash: [5, 4],
                        borderWidth: 1.5,
                        pointRadius: 0,
                        pointHitRadius: 0,
                        order: 1
                    }
                ]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                animation: false,
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        filter: item => item.dataset.label !== 'Overload',
                        callbacks: {
                            label: item => item.dataset.label === 'Median'
                                ? `Median around ${item.raw.x} ${u}: ${item.raw.y} dB`
                                : `${item.raw.x} ${u}, ${item.raw.y} dB`
                        }
                    }
                },
                scales: {
                    x: {
                        type: 'linear',
                        min: 0,
                        max: maxX,
                        grid: { color: grid },
                        ticks: { color: text },
                        border: { color: text },
                        title: { display: true, text: `Distance (${u})`, color: text }
                    },
                    y: {
                        max: 0,
                        suggestedMin: -40,
                        grid: { color: grid },
                        ticks: { color: text },
                        border: { color: text },
                        title: { display: true, text: 'Signal (dB)', color: text }
                    }
                }
            }
        });
    }

    $: if ($units && !loading && data && canvas) setTimeout(draw, 50);

    async function pick(key) {
        if (key === selected) return;
        selected = key;
        await load();
    }

    onMount(async () => {
        unsubscribe = themeChanged.subscribe(() => setTimeout(draw, 50));
        // every minute, like the notable events on the feeder's home page
        refreshTimer = setInterval(() => { if (!document.hidden) load(true); }, 60000);
        await load();
    });

    let refreshTimer = null;
    onDestroy(() => {
        clearInterval(refreshTimer);
        if (chart) chart.destroy();
        if (unsubscribe) unsubscribe();
    });
</script>

<section class="dl-card rc">
    <div class="dl-card-head">
        <h2 class="dl-h2">Signal vs distance</h2>
        <span class="dl-note">{note ? note + ' · ' : ''}{data?.points ? (data.total ?? data.points.length).toLocaleString() + (data.mode === 'first_heard' ? ' aircraft' : ' closest passes') : ''}</span>
    </div>
    <div class="flex gap-4 dl-note flex-wrap">
        <span class="flex items-center gap-1"><span class="inline-block w-2 h-2 rounded-full bg-primary"></span>{data?.mode === 'first_heard' ? 'One aircraft (when first heard)' : 'One aircraft (at its closest)'}</span>
        <span class="flex items-center gap-1"><span class="inline-block w-4 border-t-2 border-base-content"></span>Median</span>
        <span class="flex items-center gap-1"><span class="inline-block w-4 border-t-2 border-dashed border-warning"></span>Overload ({OVERLOAD_DB} dB)</span>
    </div>

    {#if loading}
        <div class="h-100 flex items-center justify-center">
            <span class="loading loading-bars loading-xl"></span>
        </div>
    {:else if error}
        <div class="alert alert-error"><span>Something went wrong: {error}</span></div>
    {:else if !hasData()}
        <div class="alert alert-info"><span>No signal readings for this period yet</span></div>
    {:else}
        <div class="h-100">
            <canvas bind:this={canvas}></canvas>
        </div>
    {/if}
    {#if data?.mode === 'first_heard'}
        <div class="alert alert-info text-sm mt-3">
            <span>Collecting closest-approach readings. Until there are enough for this period, the chart shows
            the signal when each aircraft was first heard: that is nearly always the weakest signal the receiver
            can decode, so it looks like a flat line (your detection floor) whatever the distance.
            {#if data.closest_needed}Closest-approach readings so far: {data.closest_count} of {data.closest_needed}.{/if}</span>
        </div>
    {:else}
        <p class="dl-note">
            Each dot is the signal when the aircraft was closest. A healthy band slopes down. Dots piled at the
            dashed line mean the gain is too high.
        </p>
    {/if}
    <details class="text-sm">
        <summary class="cursor-pointer opacity-80">How to read this chart</summary>
        <ul class="list-disc ml-5 mt-2 space-y-1 opacity-80">
            <li><b>Healthy:</b> a thick band running from the top left (close and strong) down to the bottom right (far and weak), with a smooth median line through it. Each dot is an aircraft's signal at its closest to you.</li>
            <li><b>Gain too high:</b> many close aircraft sit on or above the dashed overload line and the top of the band looks flattened. Lower the gain a step.</li>
            <li><b>Gain too low or a weak antenna:</b> the whole band sits low and thins out early, with few dots far away.</li>
            <li><b>Band stops at a distance:</b> that is about where hills, buildings or the horizon block the signal. Compare with Range by Direction.</li>
            <li><b>Wavy or stepped median:</b> only a few aircraft at those distances, so the median jumps around. Week or Month smooths it out.</li>
            <li><b>Gaps:</b> no aircraft were heard at those distances in this period.</li>
            <li><b>Flat line instead of a slope:</b> the chart is still showing first-heard signals (see the note above the list); it changes once enough closest-approach readings are recorded.</li>
            <li><b>Two flat bands or a jump in the line:</b> the gain changed during the period. Day shows only the newest setting.</li>
            <li><b>Close aircraft weaker than expected:</b> a vertical antenna hears poorly straight up, so aircraft passing nearly overhead read weaker than their distance suggests. Normal.</li>
            <li><b>Weak dots close in:</b> usually low aircraft partly hidden from the antenna, or weak transponders. A few are normal.</li>
        </ul>
    </details>
</section>

<style>
    .rc { display: flex; flex-direction: column; gap: 10px; }
    .rc :global(.h-100) { height: 340px; }
</style>
