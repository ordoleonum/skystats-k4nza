<script>
  // Notable events log (top of the Records tab): new range records, types
  // never seen before, aircraft back after a week or more, and round numbers
  // of different aircraft, by station day, newest first. SkyStats logs them
  // once a minute (core/k4nza-firsts.go); the feeder's home page shows today's.
  import { onMount, onDestroy } from 'svelte';
  import { units } from '../stores/units';
  import { fromNm, distanceLabel } from '../lib/units';
  import { searchFor } from '../lib/search.js';

  const PAGE = 7; // days with events per page

  let days = [];
  let today = '';
  let more = false;
  let loading = true;
  let loadingOlder = false;
  let error = null;
  let timer = null;

  async function fetchDays(before) {
    const res = await fetch(`api/k4nza/events?days=${PAGE}` + (before ? `&before=${before}` : ''));
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  }

  // the newest page; older pages already loaded stay below it
  async function load() {
    try {
      const d = await fetchDays('');
      today = d.today;
      const older = days.filter((x) => !d.days.some((y) => y.date === x.date) && x.date < (d.days[d.days.length - 1] || {}).date);
      days = [...d.days, ...older];
      if (!older.length) more = d.more;
      error = null;
    } catch (e) {
      if (!days.length) error = e.message || 'Could not load the events';
    } finally {
      loading = false;
    }
  }

  async function loadOlder() {
    if (!days.length) return;
    loadingOlder = true;
    try {
      const d = await fetchDays(days[days.length - 1].date);
      days = [...days, ...d.days];
      more = d.more;
    } catch (e) { /* keep what's shown */ }
    loadingOlder = false;
  }

  onMount(() => {
    load();
    // a minute, like the job that logs them
    timer = setInterval(() => { if (!document.hidden) load(); }, 60000);
  });
  onDestroy(() => clearInterval(timer));

  // --- words -----------------------------------------------------------------

  const DIRS = { N: 'north', NNE: 'north-northeast', NE: 'northeast', ENE: 'east-northeast', E: 'east', ESE: 'east-southeast',
    SE: 'southeast', SSE: 'south-southeast', S: 'south', SSW: 'south-southwest', SW: 'southwest', WSW: 'west-southwest',
    W: 'west', WNW: 'west-northwest', NW: 'northwest', NNW: 'north-northwest' };
  const TAGS = { range: ['Record', 'rec'], type: ['First', 'new'], back: ['Back', 'back'], milestone: ['Milestone', 'mile'] };
  const nf = new Intl.NumberFormat();

  // like the feeder: whole numbers from 10 up, one decimal below
  function distParts(nm) {
    const v = fromNm(nm, $units);
    return [v < 10 ? Math.round(v * 10) / 10 : Math.round(v), distanceLabel[$units]];
  }
  const dist = (nm) => distParts(nm).join(' ');
  const who = (a) => a.registration || a.callsign || (a.hex || '').toUpperCase();
  const name = (a) => a.model || a.type || '';

  function words(e) {
    const a = e.aircraft || {};
    switch (e.kind) {
      case 'range': {
        const [now, u] = distParts(e.distance_nm);
        const [was] = distParts(e.old_nm);
        const gain = Math.round((now - was) * 10) / 10;
        return { what: `Farthest ever to the ${DIRS[e.direction] || e.direction}: ${dist(e.distance_nm)}`,
          sub: `Old best ${was} ${u}${gain > 0 ? ` (+${gain} ${u})` : ''}. ${who(a)}${name(a) ? ` (${name(a)})` : ''}` };
      }
      case 'type':
        return { what: `First ${name(a) || a.type}${a.model && a.type ? ` (${a.type})` : ''} seen`,
          sub: [who(a), a.operator].filter(Boolean).join(', ') };
      case 'back':
        return { what: `${who(a)} back after ${e.days} ${e.days === 1 ? 'day' : 'days'}`,
          sub: [name(a), a.operator].filter(Boolean).join(', ') };
      case 'milestone':
        return { what: `${nf.format(e.number)}th different aircraft`,
          sub: a.hex ? `${who(a)}${name(a) ? ` (${name(a)})` : ''}` : '' };
    }
    return { what: '', sub: '' };
  }

  const dayDate = (s) => new Date(s + 'T12:00');
  function dayTitle(s) {
    if (s === today) return 'Today';
    if (today && (dayDate(today) - dayDate(s)) / 86400000 === 1) return 'Yesterday';
    return dayDate(s).toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' });
  }
  function time(local) {
    const m = /T(\d\d):(\d\d)/.exec(local || '');
    if (!m) return '';
    return new Date(2000, 0, 1, Number(m[1]), Number(m[2])).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
  }
  function open(e) {
    const a = e.aircraft;
    if (a && a.hex) searchFor(a.registration || a.hex.toUpperCase());
  }
</script>

<section class="dl-card ne">
  <div class="dl-card-head">
    <h2 class="dl-h2">Notable events</h2>
    <span class="dl-note">New records, firsts, returns and milestones</span>
  </div>
  {#if loading}
    <div class="skeleton h-24 w-full mt-4"></div>
  {:else if error}
    <p class="dl-note mt-3">{error}</p>
  {:else if !days.length}
    <p class="dl-note mt-3">Nothing yet. New range records, aircraft types seen for the first time and aircraft back
      after a week away show up here once your station has a week of history; round numbers of different aircraft
      show up from the start.</p>
  {:else}
    {#each days as d (d.date)}
      <h3 class="ne-day">{dayTitle(d.date)}</h3>
      <ul class="ne-list">
        {#each d.events as e}
          {@const w = words(e)}
          <li>
            <button type="button" class="ne-row" class:ne-link={e.aircraft && e.aircraft.hex} on:click={() => open(e)}
              title={e.aircraft && e.aircraft.hex ? `Search for ${who(e.aircraft)}` : ''}>
              <span class="ne-tag ne-{(TAGS[e.kind] || ['', ''])[1]}">{(TAGS[e.kind] || [e.kind])[0]}</span>
              <span class="ne-text">
                <span class="ne-what">{w.what}</span>
                {#if w.sub}<span class="ne-sub">{w.sub}</span>{/if}
              </span>
              <span class="ne-when dl-mono">{time(e.when)}</span>
            </button>
          </li>
        {/each}
      </ul>
    {/each}
    {#if more}
      <button type="button" class="btn btn-sm btn-ghost mt-3" on:click={loadOlder} disabled={loadingOlder}>
        {loadingOlder ? 'Loading…' : 'Show older days'}
      </button>
    {/if}
  {/if}
</section>

<style>
  .ne { margin-top: 2.5rem; }
  .ne-day { font-size: 13px; font-weight: 700; color: var(--dl-muted); text-transform: uppercase; letter-spacing: 0.04em;
    margin: 18px 0 4px; }
  .ne-list { list-style: none; margin: 0; padding: 0; }
  .ne-row { width: 100%; display: grid; grid-template-columns: 92px minmax(0, 1fr) auto; gap: 12px; align-items: center;
    padding: 10px 0; border: 0; border-top: 1px solid var(--dl-line-2); background: none; color: inherit; text-align: left;
    font: inherit; cursor: default; }
  .ne-list li:first-child .ne-row { border-top: 0; }
  .ne-link { cursor: pointer; }
  .ne-link:hover .ne-what { color: rgb(var(--dl-accent-rgb)); }
  .ne-tag { justify-self: start; font-size: 12px; font-weight: 700; padding: 2px 10px; border-radius: 999px; white-space: nowrap; }
  /* the same colours as the feeder's notable events card */
  .ne-rec { background: var(--dl-mil-soft); color: var(--dl-mil-ink); }
  .ne-new { background: var(--dl-accent-soft); color: var(--dl-accent-ink); }
  .ne-back { background: var(--dl-civ-soft); color: var(--dl-civ-ink); }
  .ne-mile { background: rgba(22, 163, 74, 0.14); color: #15803d; }
  :global([data-theme='daylight-dark']) .ne-mile { background: rgba(74, 222, 128, 0.14); color: #4ade80; }
  .ne-text { display: flex; flex-direction: column; min-width: 0; }
  .ne-what { font-weight: 600; line-height: 1.3; }
  .ne-sub { font-size: 13px; color: var(--dl-muted); line-height: 1.3; margin-top: 2px; }
  .ne-when { font-size: 13px; color: var(--dl-muted); white-space: nowrap; }
  @media (max-width: 640px) {
    .ne-row { grid-template-columns: minmax(0, 1fr) auto; gap: 4px 10px; }
    .ne-tag { grid-column: 1 / -1; }
  }
</style>
