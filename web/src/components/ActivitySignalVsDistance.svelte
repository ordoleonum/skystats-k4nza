<script>
// @ts-nocheck
    // Signal strength (RSSI) against distance, with the median per 10 nm.
    import { onMount, onDestroy } from 'svelte';
    import { themeChanged } from '../lib/themeStore.js';
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
    let selected = 'day';

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

    async function load() {
        loading = true;
        error = null;
        try {
            const res = await fetch(`api/stats/reception/signal/${selected}`);
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

        const maxX = Math.max(50, Math.ceil(Math.max(...data.points.map(p => p[0])) / 25) * 25);

        if (chart) chart.destroy();
        chart = new Chart(canvas, {
            type: 'scatter',
            data: {
                datasets: [
                    {
                        label: 'Aircraft',
                        data: data.points.map(p => ({ x: p[0], y: p[1] })),
                        backgroundColor: withAlpha(primary, 0.45),
                        pointRadius: 2.5,
                        pointHoverRadius: 4,
                        order: 3
                    },
                    {
                        type: 'line',
                        label: 'Median',
                        data: data.median.map(p => ({ x: p[0], y: p[1] })),
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
                                ? `Median around ${item.raw.x} nm: ${item.raw.y} dB`
                                : `${item.raw.x} nm, ${item.raw.y} dB`
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
                        title: { display: true, text: 'Distance (nm)', color: text }
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

    $: if (!loading && data && canvas) setTimeout(draw, 50);

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

<div class="bg-base-100 rounded-box shadow-md pt-6 p-8">
    <div class="flex justify-between items-center mb-2 gap-2 flex-wrap">
        <h2 class="text-2xl font-extralight tracking-wider">Signal vs Distance</h2>
        <div class="tabs tabs-box">
            {#each periods as p}
                <input type="radio" name="activity_tabs_signal" class="tab" aria-label={p.label}
                    checked={selected === p.key} on:change={() => pick(p.key)} />
            {/each}
        </div>
    </div>
    <div class="flex gap-4 text-xs opacity-70 mb-2 flex-wrap">
        <span class="flex items-center gap-1"><span class="inline-block w-2 h-2 rounded-full bg-primary"></span>One aircraft (when first heard)</span>
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
    <p class="text-xs opacity-60 mt-3">
        A healthy band slopes down. Dots piled at the dashed line mean the gain is too high.
    </p>
</div>
