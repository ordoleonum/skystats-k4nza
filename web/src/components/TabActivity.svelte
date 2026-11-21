<script>
  // The Activity tab as a report (Daylight design): what crossed your sky
  // today, this week, this month or ever. One request (api/k4nza/report) fills
  // everything except the two reception charts, which keep their own data.
  import { onMount, onDestroy } from 'svelte';
  import ActivityRangeByDirection from './ActivityRangeByDirection.svelte';
  import ActivitySignalVsDistance from './ActivitySignalVsDistance.svelte';
  import AboveTimeline from './AboveTimeline.svelte';
  import { units } from '../stores/units';
  import { fromNm, distanceLabel } from '../lib/units';
  import { replayUrl } from '../lib/feeder.js';
  import { searchFor } from '../lib/search.js';

  export let active = true;

  const PERIODS = [
    { key: 'today', label: 'Today' },
    { key: 'week', label: 'Week' },
    { key: 'month', label: 'Month' },
    { key: 'all', label: 'All time' }
  ];
  const DAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
  const GROUPS = { Mil: 'Military', Gov: 'Government', Pol: 'Police', Civ: 'Civilian' };
  const DIRS = { N: 'north', NNE: 'north-northeast', NE: 'northeast', ENE: 'east-northeast', E: 'east', ESE: 'east-southeast',
    SE: 'southeast', SSE: 'south-southeast', S: 'south', SSW: 'south-southwest', SW: 'southwest', WSW: 'west-southwest',
    W: 'west', WNW: 'west-northwest', NW: 'northwest', NNW: 'north-northwest' };

  let period = 'week';
  try {
    const saved = localStorage.getItem('k4nza-report-period');
    if (PERIODS.some((p) => p.key === saved)) period = saved;
  } catch (e) { /* private window */ }

  let data = null;
  let loading = true;
  let error = null;
  let timer = null;
  let photos = {};

  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  const nf = new Intl.NumberFormat();
  const num = (n) => nf.format(Math.round(n || 0));

  async function load() {
    const want = period;
    try {
      const res = await fetch(`api/k4nza/report?period=${want}&tz=${encodeURIComponent(tz)}`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const d = await res.json();
      if (want !== period) return; // switched while loading
      data = d;
      error = null;
      loadPhotos(d.visitors || []);
    } catch (e) {
      if (want === period) error = e.message || 'Could not load the report';
    } finally {
      if (want === period) loading = false;
    }
  }

  function pick(key) {
    if (key === period) return;
    period = key;
    loading = true;
    data = null;
    try { localStorage.setItem('k4nza-report-period', key); } catch (e) { /* private window */ }
    load();
  }

  // a photo for each notable visitor (planespotters.net, as on the Search tab)
  async function loadPhotos(list) {
    for (const v of list) {
      if (!v.hex || v.hex in photos) continue;
      photos[v.hex] = null;
      try {
        const res = await fetch(`https://api.planespotters.net/pub/photos/hex/${encodeURIComponent(v.hex)}`);
        if (!res.ok) continue;
        const j = await res.json();
        const p = j.photos && j.photos[0];
        if (p && p.thumbnail_large) {
          photos[v.hex] = { src: p.thumbnail_large.src, link: p.link, by: p.photographer };
          photos = photos;
        }
      } catch (e) { /* no photo */ }
    }
  }

  onMount(() => {
    load();
    timer = setInterval(() => { if (active && !document.hidden) load(); }, 60000);
  });
  onDestroy(() => clearInterval(timer));

  // --- words -----------------------------------------------------------------

  const day = (s) => new Date(s + 'T12:00');
  const monthDay = (s) => day(s).toLocaleDateString(undefined, { month: 'long', day: 'numeric' });
  function dist(nm, digits = 0) {
    if (nm == null) return '';
    const v = fromNm(nm, $units);
    return `${v.toLocaleString(undefined, { maximumFractionDigits: digits })} ${distanceLabel[$units]}`;
  }
  function when(local) {
    if (!local) return '';
    const d = new Date(local);
    if (isNaN(d)) return '';
    const sameDay = data && local.slice(0, 10) === data.today;
    const t = d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
    if (sameDay) return t;
    const days = (day(data.today) - day(local.slice(0, 10))) / 86400000;
    return (days < 7 ? d.toLocaleDateString(undefined, { weekday: 'short' }) : d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })) + ' ' + t;
  }

  $: label = !data ? '' :
    period === 'today' ? day(data.today).toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' }) :
    period === 'week' ? `Week of ${monthDay(data.start)} – ${day(data.today).getMonth() === day(data.start).getMonth() ? day(data.today).getDate() : monthDay(data.today)}` :
    period === 'month' ? `Last 30 days · ${monthDay(data.start)} – ${monthDay(data.today)}` :
    `Since ${day(data.start).toLocaleDateString(undefined, { month: 'long', day: 'numeric', year: 'numeric' })}`;

  $: span = { today: 'today', week: 'this week', month: 'in the last 30 days', all: 'since SkyStats started' }[period];
  $: before = { today: 'yesterday by this time', week: 'last week', month: 'the 30 days before' }[period];

  $: headline = data ? `${num(data.flights)} ${data.flights === 1 ? 'flight' : 'flights'} crossed your sky ${span}.` : '';
  $: story = !data || !$units ? '' : (() => {
    const parts = [];
    if (data.flights_before != null && data.flights_before > 0) {
      const pct = Math.round((data.flights / data.flights_before - 1) * 100);
      parts.push(Math.abs(pct) < 1 ? `About the same as ${before}.` : `That's ${Math.abs(pct)}% ${pct > 0 ? 'more' : 'fewer'} than ${before}.`);
    }
    const bits = [];
    bits.push(data.military ? `${num(data.military)} ${data.military === 1 ? 'was' : 'were'} military` : 'none were military');
    if (period !== 'all') bits.push(`${num(data.new_aircraft)} aircraft ${data.new_aircraft === 1 ? 'was' : 'were'} new to your station`);
    if (data.farthest && data.farthest.distance_nm) bits.push(`the farthest was ${dist(data.farthest.distance_nm)} away`);
    if (bits.length) {
      const s = bits.length > 1 ? bits.slice(0, -1).join(', ') + ', and ' + bits[bits.length - 1] : bits[0];
      parts.push(s.charAt(0).toUpperCase() + s.slice(1) + '.');
    }
    return parts.join(' ');
  })();

  $: numbers = !data || !$units ? [] : [
    { value: num(data.aircraft), label: 'Different aircraft',
      note: period === 'all' ? `${num(data.flights)} visits in all` : `${num(data.new_aircraft)} seen for the first time` },
    { value: num(data.types), label: 'Aircraft types',
      note: data.top_type ? `${data.top_type.name || data.top_type.code} the most common` : 'None yet' },
    { value: data.farthest && data.farthest.distance_nm ? dist(data.farthest.distance_nm) : '–', label: 'Farthest',
      note: data.farthest && data.farthest.distance_nm
        ? `${period === 'today' ? 'Today' : when(data.farthest.when).replace(/ \d.*$/, '')}, to the ${DIRS[data.farthest.direction] || data.farthest.direction}`
        : 'No positions yet' },
    { value: num(data.military), label: 'Military',
      note: data.military_types && data.military_types.length ? data.military_types.join(', ') : (data.military ? 'From the feeder\'s database' : `None ${span}`) }
  ];

  // --- when the sky is busiest --------------------------------------------------

  $: heatRows = !data ? [] : (() => {
    const t = data.today_weekday;
    let order;
    if (period === 'today') order = [t];
    else if (period === 'week') order = Array.from({ length: 7 }, (_, i) => (t + 1 + i) % 7); // ends today
    else order = [0, 1, 2, 3, 4, 5, 6];
    return order.map((d) => ({ d, label: DAYS[d], cells: data.heat[d] || [] }));
  })();
  $: heatMax = Math.max(1, ...heatRows.flatMap((r) => r.cells));
  $: averaged = period === 'month' || period === 'all';
  const hourName = (h) => new Date(2000, 0, 1, h).toLocaleTimeString(undefined, { hour: 'numeric' });
  function cellTip(r, h, n) {
    const what = averaged ? `${n.toLocaleString(undefined, { maximumFractionDigits: 1 })} flights on an average ${r.label}` : `${num(n)} ${n === 1 ? 'flight' : 'flights'}`;
    return `${r.label} ${hourName(h)}: ${what}`;
  }
  $: busiest = !heatRows.length ? null : (() => {
    let best = null;
    for (const r of heatRows) r.cells.forEach((n, h) => { if (n > 0 && (!best || n > best.n)) best = { r, h, n }; });
    return best;
  })();

  // --- bars --------------------------------------------------------------------

  $: airlines = data ? data.airlines || [] : [];
  $: airlineMax = Math.max(1, ...airlines.map((a) => a.flights));
  $: types = data ? data.aircraft_types || [] : [];
  $: typeMax = Math.max(1, ...types.map((t) => t.aircraft));
  $: routes = data ? data.routes || [] : [];
  $: visitors = data ? data.visitors || [] : [];

  $: rangePeriod = period === 'today' ? 'day' : period;
  $: signalPeriod = period === 'today' ? 'day' : period === 'all' ? 'month' : period;

  function openInteresting(e) {
    e.preventDefault();
    window.dispatchEvent(new CustomEvent('skystats-open-tab', { detail: 'interesting-stat' }));
  }
  function title(v) {
    return v.model || v.type || v.registration || v.hex.toUpperCase();
  }
  function sub(v) {
    const s = [];
    if (v.operator) s.push(v.operator);
    else if (v.registration) s.push(v.registration);
    if (v.distance_nm != null) s.push(`closest ${dist(v.distance_nm, 1)}`);
    return s.join(' · ');
  }
</script>

<div class="rep">
  <section class="rep-hero">
    <div class="rep-hero-text">
      <div class="rep-label">{label || ' '}</div>
      {#if data}
        <h1 class="rep-h1">{headline}</h1>
        <p class="rep-story">{story}</p>
      {:else if error}
        <h1 class="rep-h1">The report isn't available right now.</h1>
        <p class="rep-story">SkyStats could not answer ({error}). It retries every minute.</p>
      {:else}
        <h1 class="rep-h1 rep-wait">Counting your flights…</h1>
      {/if}
    </div>
    <div class="dl-seg" role="group" aria-label="Period">
      {#each PERIODS as p}
        <button type="button" aria-pressed={period === p.key} on:click={() => pick(p.key)}>{p.label}</button>
      {/each}
    </div>
  </section>

  <div class="rep-numbers">
    {#each (numbers.length ? numbers : [0, 1, 2, 3]) as n}
      <div class="dl-card dl-card-sm">
        {#if n && n.label}
          <div class="rep-num dl-mono">{n.value}</div>
          <div class="rep-num-label">{n.label}</div>
          <div class="dl-note rep-num-note">{n.note}</div>
        {:else}
          <div class="skeleton h-10 w-28 mb-2"></div><div class="skeleton h-4 w-40"></div>
        {/if}
      </div>
    {/each}
  </div>

  <section class="dl-card">
    <div class="dl-card-head">
      <h2 class="dl-h2">When your sky is busiest</h2>
      <div class="rep-legend dl-note">Quiet <span class="rep-legend-bar"></span> Busy</div>
    </div>
    {#if busiest}
      <p class="dl-note rep-sub">{period === 'today' ? 'Busiest hour so far' : averaged ? 'Usually busiest' : 'Busiest'}: {busiest.r.label} around {hourName(busiest.h)}.{#if averaged} Each square is the average for that weekday and hour.{/if}</p>
    {/if}
    <div class="rep-heat-scroll">
      <div class="rep-heat" role="img" aria-label="Flights by day and hour">
        {#each heatRows as r}
          <div class="rep-heat-row">
            <span class="rep-heat-day">{r.label}</span>
            {#each r.cells as n, h}
              <span class="rep-cell" title={cellTip(r, h, n)}
                style="background: rgba(var(--dl-accent-rgb), {(0.08 + Math.min(1, n / heatMax) * 0.92).toFixed(2)})"></span>
            {/each}
          </div>
        {:else}
          {#if loading}<div class="skeleton h-40 w-full"></div>{/if}
        {/each}
        <div class="rep-heat-hours dl-mono"><span></span><span>{hourName(0)}</span><span>{hourName(6)}</span><span>{hourName(12)}</span><span>{hourName(18)}</span></div>
      </div>
    </div>
  </section>

  <div class="rep-two">
    <ActivityRangeByDirection period={rangePeriod} />
    <ActivitySignalVsDistance period={signalPeriod} note={period === 'all' ? 'Last 30 days' : ''} />
  </div>

  <div class="rep-two">
    <section class="dl-card">
      <h2 class="dl-h2 rep-mb">Who flew over most</h2>
      {#if airlines.length}
        <div class="rep-bars" role="list">
          {#each airlines as a}
            <div class="rep-bar-row" role="listitem" title="{a.name}: {num(a.flights)} flights">
              <span class="rep-bar-name">{a.name}</span>
              <span class="rep-bar-track"><span class="rep-bar" style="width: {Math.max(2, Math.round(a.flights / airlineMax * 100))}%"></span></span>
              <span class="rep-bar-n dl-mono">{num(a.flights)}</span>
            </div>
          {/each}
        </div>
        <p class="dl-note rep-foot">Flights with a known route, by airline.</p>
      {:else if !loading}
        <p class="dl-note">No flights with a known airline {span} yet.</p>
      {/if}
    </section>

    <section class="dl-card">
      <h2 class="dl-h2 rep-mb">Aircraft types</h2>
      {#if types.length}
        <div class="rep-bars" role="list">
          {#each types as t}
            <div class="rep-bar-row" role="listitem">
              <span class="rep-bar-name">
                <button type="button" class="rep-type dl-mono" title="Search for {t.code}" on:click={() => searchFor(t.code)}>{t.code}</button>
                {#if t.name}<span class="rep-type-name">{t.name}</span>{/if}
              </span>
              <span class="rep-bar-track"><span class="rep-bar" style="width: {Math.max(2, Math.round(t.aircraft / typeMax * 100))}%"></span></span>
              <span class="rep-bar-n dl-mono">{num(t.aircraft)}</span>
            </div>
          {/each}
        </div>
        <p class="dl-note rep-foot">Different aircraft of each type.</p>
      {:else if !loading}
        <p class="dl-note">No aircraft types {span} yet.</p>
      {/if}
    </section>
  </div>

  <section class="dl-card">
    <h2 class="dl-h2">Where they were going</h2>
    {#if routes.length}
      <div class="rep-routes">
        {#each routes as r}
          <div class="rep-route" title="{r.from_name || r.from} → {r.to_name || r.to}: {num(r.flights)} flights">
            <span class="rep-iata dl-mono">{r.from}</span>
            <span class="rep-route-line"><span></span></span>
            <span class="rep-iata dl-mono">{r.to}</span>
            <span class="rep-route-n">{num(r.flights)}</span>
          </div>
        {/each}
      </div>
    {:else if !loading}
      <p class="dl-note rep-sub">No routes looked up {span} yet.</p>
    {/if}
  </section>

  <section>
    <div class="dl-card-head rep-mb">
      <h2 class="dl-h2">Notable visitors</h2>
      <a href="./" class="dl-link" on:click={openInteresting}>All interesting aircraft</a>
    </div>
    {#if visitors.length}
      <div class="rep-visitors">
        {#each visitors as v}
          <article class="rep-visitor">
            <div class="rep-photo">
              {#if photos[v.hex]}
                <a href={photos[v.hex].link} target="_blank" rel="noopener noreferrer" title="Photo: {photos[v.hex].by} (planespotters.net)">
                  <img src={photos[v.hex].src} alt="{title(v)}" loading="lazy" />
                </a>
              {:else}
                <span>No photo</span>
              {/if}
            </div>
            <div class="rep-visitor-body">
              <span class="dl-tag dl-tag-{v.group}">{GROUPS[v.group] || v.group}</span>
              <button type="button" class="rep-visitor-title" title="Search for this aircraft" on:click={() => searchFor(v.registration || v.hex)}>{title(v)}</button>
              <div class="dl-note rep-visitor-sub">{sub(v)}</div>
              {#if replayUrl(v.hex, v.replay_utc)}
                <a class="dl-link rep-replay" href={replayUrl(v.hex, v.replay_utc)} target="_blank" rel="noopener"
                  title="Replay this visit on the live map">{when(v.seen)} · Replay ▸</a>
              {/if}
            </div>
          </article>
        {/each}
      </div>
    {:else if !loading}
      <div class="dl-card"><p class="dl-note">No military, government, police or other interesting aircraft {span}.</p></div>
    {/if}
  </section>

  <section class="rep-above">
    <h2 class="dl-h2 rep-mb">Overhead right now</h2>
    <AboveTimeline />
  </section>
</div>

<style>
  .rep { display: flex; flex-direction: column; gap: 32px; }
  .rep-hero { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: flex-end; gap: 16px; }
  .rep-hero-text { flex: 1 1 560px; min-width: 0; }
  .rep-label { font-weight: 700; color: var(--dl-accent); min-height: 1.4em; }
  .rep-h1 { margin: 6px 0 0; font-size: 52px; line-height: 1.05; font-weight: 800; letter-spacing: -0.03em; }
  .rep-wait { color: var(--dl-muted); }
  .rep-story { margin: 12px 0 0; font-size: 18px; color: var(--dl-muted); max-width: 640px; line-height: 1.5; }
  .rep-numbers { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; }
  @media (max-width: 1000px) { .rep-numbers { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
  .rep-num { font-size: 34px; font-weight: 600; letter-spacing: -0.02em; line-height: 1.2; }
  .rep-num-label { font-weight: 700; margin-top: 2px; }
  .rep-num-note { margin-top: 2px; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; }
  .rep-sub { margin: 6px 0 0; }
  .rep-mb { margin-bottom: 18px; }
  .rep-foot { margin: 14px 0 0; }

  .rep-legend { display: flex; align-items: center; gap: 8px; }
  .rep-legend-bar { width: 120px; height: 10px; border-radius: 5px;
    background: linear-gradient(90deg, rgba(var(--dl-accent-rgb), 0.08), rgba(var(--dl-accent-rgb), 1)); }
  .rep-heat-scroll { overflow-x: auto; margin-top: 18px; }
  .rep-heat { min-width: 680px; display: flex; flex-direction: column; gap: 4px; }
  .rep-heat-row { display: grid; grid-template-columns: 44px repeat(24, minmax(0, 1fr)); gap: 4px; align-items: center; }
  .rep-heat-day { font-size: 13px; font-weight: 600; color: var(--dl-ink-2); }
  .rep-cell { display: block; height: 22px; border-radius: 4px; }
  .rep-heat-hours { display: grid; grid-template-columns: 44px repeat(4, minmax(0, 1fr)); font-size: 12px; color: var(--dl-muted); margin-top: 4px; }

  .rep-two { display: grid; grid-template-columns: repeat(auto-fit, minmax(380px, 1fr)); gap: 16px; }
  .rep-two > :global(*) { min-width: 0; }

  .rep-bars { display: flex; flex-direction: column; gap: 12px; }
  .rep-bar-row { display: grid; grid-template-columns: 112px minmax(0, 1fr) 56px; gap: 12px; align-items: center; }
  .rep-bar-name { font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
  .rep-bar-track { height: 22px; border-radius: 6px; background: var(--dl-accent-soft); overflow: hidden; }
  .rep-bar { display: block; height: 100%; border-radius: 6px; background: var(--dl-accent); }
  .rep-bar-n { font-weight: 600; text-align: right; }
  .rep-type { display: block; font-weight: 600; background: none; border: 0; padding: 0; color: inherit; cursor: pointer; }
  .rep-type:hover { color: var(--dl-accent); }
  .rep-type-name { display: block; font-size: 12px; font-weight: 500; color: var(--dl-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  .rep-routes { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); column-gap: 40px; margin-top: 8px; }
  .rep-route { display: grid; grid-template-columns: 52px minmax(0, 1fr) 52px 56px; gap: 10px; align-items: center; padding: 12px 0; border-top: 1px solid var(--dl-line-2); }
  .rep-iata { font-weight: 600; font-size: 17px; }
  .rep-route-line { height: 2px; background: var(--dl-accent-mid); position: relative; }
  .rep-route-line span { position: absolute; right: -2px; top: -4px; width: 10px; height: 10px; border-radius: 50%; background: var(--dl-accent); }
  .rep-route-n { text-align: right; font-weight: 700; }

  .rep-visitors { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 16px; }
  .rep-visitor { border-radius: 20px; background: var(--dl-surface); border: 1px solid var(--dl-line); overflow: hidden; display: flex; flex-direction: column; }
  .rep-photo { height: 140px; background: var(--dl-photo); display: flex; align-items: center; justify-content: center; color: var(--dl-muted); font-size: 13px; font-weight: 600; }
  .rep-photo a, .rep-photo img { display: block; width: 100%; height: 100%; }
  .rep-photo img { object-fit: cover; }
  .rep-visitor-body { padding: 16px 18px; display: flex; flex-direction: column; gap: 6px; flex: 1; }
  .rep-visitor-title { font-size: 18px; font-weight: 800; text-align: left; background: none; border: 0; padding: 0; color: inherit; cursor: pointer; }
  .rep-visitor-title:hover { color: var(--dl-accent); }
  .rep-visitor-sub { font-size: 14px; }
  .rep-replay { margin-top: auto; padding-top: 8px; }

  @media (max-width: 760px) {
    .rep { gap: 24px; }
    .rep-h1 { font-size: 34px; }
    .rep-story { font-size: 16px; }
    .rep-two { grid-template-columns: minmax(0, 1fr); }
    .rep-numbers { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
    .rep-num { font-size: 26px; }
    .rep-routes { grid-template-columns: minmax(0, 1fr); }
    .rep-bar-row { grid-template-columns: 96px minmax(0, 1fr) 48px; gap: 10px; }
  }
</style>
