<script>
// @ts-nocheck
    // Farthest aircraft heard in each 10° slice around the antenna.
    import { onMount, onDestroy } from 'svelte';
    import { themeChanged } from '../lib/themeStore.js';
    import { units } from '../stores/units';
    import { fromNm, round, distanceLabel } from '../lib/units';
    import {
        Chart,
        RadarController,
        RadialLinearScale,
        PointElement,
        LineElement,
        Filler,
        Tooltip
    } from 'chart.js';

    Chart.register(RadarController, RadialLinearScale, PointElement, LineElement, Filler, Tooltip);

    const periods = [
        { key: 'day', label: '24hrs' },
        { key: 'week', label: 'Week' },
        { key: 'month', label: 'Month' },
        { key: 'all', label: 'All' }
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

    const compass = { 0: 'N', 45: 'NE', 90: 'E', 135: 'SE', 180: 'S', 225: 'SW', 270: 'W', 315: 'NW' };

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

    async function load() {
        loading = true;
        error = null;
        try {
            const res = await fetch(`api/stats/reception/range/${selected}`);
            if (!res.ok) throw new Error(`HTTP ${res.status}`);
            data = await res.json();
        } catch (e) {
            error = e.message || 'Failed to load data';
            data = null;
        } finally {
            loading = false;
        }
    }

    function hasData() {
        return data && data.period && data.period.some(v => v > 0);
    }

    function draw() {
        if (!canvas || !hasData()) return;
        if (canvas.offsetWidth === 0) {
            requestAnimationFrame(draw);
            return;
        }
        const primary = daisyColor('bg-primary');
        const text = daisyColor('bg-base-content');
        const grid = withAlpha(text, 0.15);
        const step = data.sector_degrees;
        const labels = data.period.map((_, i) => compass[i * step] ?? '');
        const u = distanceLabel[$units];
        const conv = (arr) => arr.map(v => round(fromNm(v, $units), 1));
        const showAllTime = selected !== 'all';

        const datasets = [];
        if (showAllTime) {
            datasets.push({
                label: 'All-time best',
                data: conv(data.all_time),
                borderColor: withAlpha(text, 0.5),
                borderDash: [4, 4],
                borderWidth: 1.5,
                pointRadius: 0,
                pointHitRadius: 6,
                fill: false
            });
        }
        datasets.push({
            label: periods.find(p => p.key === selected).label,
            data: conv(data.period),
            borderColor: primary,
            backgroundColor: withAlpha(primary, 0.2),
            borderWidth: 2,
            pointRadius: 0,
            pointHitRadius: 6,
            fill: true
        });

        if (chart) chart.destroy();
        chart = new Chart(canvas, {
            type: 'radar',
            data: { labels, datasets },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        callbacks: {
                            title: items => {
                                const from = items[0].dataIndex * step;
                                return `${from}°–${from + step}°`;
                            },
                            label: item => `${item.dataset.label}: ${item.raw} ${u}`
                        }
                    }
                },
                scales: {
                    r: {
                        beginAtZero: true,
                        angleLines: { color: grid },
                        grid: { color: grid },
                        pointLabels: { color: text, font: { size: 13 } },
                        ticks: {
                            color: text,
                            backdropColor: 'transparent',
                            maxTicksLimit: 5,
                            callback: v => `${v} ${u}`
                        }
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
        await load();
    });

    onDestroy(() => {
        if (chart) chart.destroy();
        if (unsubscribe) unsubscribe();
    });
</script>

<section class="dl-card rc">
    <div class="dl-card-head">
        <h2 class="dl-h2">How far you can hear</h2>
        <span class="dl-note">Range by direction</span>
    </div>
    <div class="flex gap-4 dl-note flex-wrap">
        <span class="flex items-center gap-1"><span class="inline-block w-3 h-3 rounded-sm bg-primary"></span>
            Farthest heard{#if data && data.max_nm}{' '}(max {round(fromNm(data.max_nm, $units), 1)} {distanceLabel[$units]}){/if}</span>
        {#if selected !== 'all'}
            <span class="flex items-center gap-1"><span class="inline-block w-4 border-t-2 border-dashed border-base-content"></span>All-time best</span>
        {/if}
    </div>

    {#if loading}
        <div class="h-100 flex items-center justify-center">
            <span class="loading loading-bars loading-xl"></span>
        </div>
    {:else if error}
        <div class="alert alert-error"><span>Something went wrong: {error}</span></div>
    {:else if !hasData()}
        <div class="alert alert-info"><span>No positions recorded for this period yet</span></div>
    {:else}
        <div class="h-100">
            <canvas bind:this={canvas}></canvas>
        </div>
    {/if}
    <p class="dl-note">
        A dent in the shape means something is blocking the antenna in that direction.
    </p>
    <details class="text-sm">
        <summary class="cursor-pointer opacity-80">How to read this chart</summary>
        <ul class="list-disc ml-5 mt-2 space-y-1 opacity-80">
            <li><b>The shape:</b> the farthest aircraft heard in each direction, with your station in the middle. Bigger means more range.</li>
            <li><b>Healthy:</b> a fairly round shape that reaches about as far as the radio horizon (usually 200 to 250 mi for airliners at cruise height).</li>
            <li><b>Dent or flat side:</b> something blocks the antenna that way: a roof, chimney, tree, hill or wiring. A higher or clearer antenna spot helps.</li>
            <li><b>Spikes:</b> a few aircraft heard unusually far, often during unusual weather (tropospheric ducting). Not a fault.</li>
            <li><b>Smaller than the dashed line:</b> fewer or lower aircraft in this period, or reception got worse. If it stays small for days, check the antenna, cable and gain.</li>
            <li><b>Lopsided towards busy airways:</b> normal; directions with few flights fill in more slowly. Month and All smooth it out.</li>
        </ul>
    </details>
</section>

<style>
    .rc { display: flex; flex-direction: column; gap: 10px; }
    .rc :global(.h-100) { height: 340px; }
</style>
