<script>
  // Notable events log (top of the Records tab): new range records, the
  // busiest day so far, types, airlines and countries never seen before, and
  // round numbers of different aircraft, by station day, newest first.
  // SkyStats logs them once a minute (core/k4nza-firsts.go); the feeder's home
  // page shows today's.
  //
  // Compact: each day is one summary line ("Today · 1 record, 4 firsts"),
  // today open; inside a day the best of each kind shows and the rest fold
  // under "Show 3 more", as on the feeder's card.
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
  let openDays = {}; // date -> true (today starts open)
  let openKinds = {}; // date|kind -> true

  async function fetchDays(before) {
    const res = await fetch(`api/k4nza/events?days=${PAGE}` + (before ? `&before=${before}` : ''));
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  }

  // the newest page; older pages already loaded stay below it
  async function load() {
    try {
      const d = await fetchDays('');
      if (!today) openDays[d.today] = true;
      today = d.today;
      const oldest = (d.days[d.days.length - 1] || {}).date || '';
      const older = days.filter((x) => x.date < oldest);
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
  // tag, colour, and the words for "Show 3 more ..."
  const KINDS = {
    range: ['Record', 'rec', 'record', 'records'],
    busiest: ['Record', 'rec', 'record', 'records'],
    type: ['First', 'new', 'new type', 'new types'],
    airline: ['First', 'new', 'new airline', 'new airlines'],
    country: ['First', 'new', 'new country', 'new countries'],
    milestone: ['Milestone', 'mile', 'milestone', 'milestones'],
  };
  const nf = new Intl.NumberFormat();

  // like the feeder: whole numbers from 10 up, one decimal below
  function distParts(nm) {
    const v = fromNm(nm, $units);
    return [v < 10 ? Math.round(v * 10) / 10 : Math.round(v), distanceLabel[$units]];
  }
  const dist = (nm) => distParts(nm).join(' ');
  const who = (a) => a.registration || a.callsign || (a.hex || '').toUpperCase();
  const name = (a) => a.model || a.type || '';
  const dayDate = (s) => new Date(s + 'T12:00');
  const shortDay = (s) => dayDate(s).toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' });

  function words(e, date) {
    const a = e.aircraft || {};
    switch (e.kind) {
      case 'range': {
        const [now, u] = distParts(e.distance_nm);
        const [was] = distParts(e.old_nm);
        const gain = Math.round((now - was) * 10) / 10;
        return { what: `Farthest ever to the ${DIRS[e.direction] || e.direction}: ${dist(e.distance_nm)}`,
          sub: `Old best ${was} ${u}${gain > 0 ? ` (+${gain} ${u})` : ''}. ${who(a)}${name(a) ? ` (${name(a)})` : ''}` };
      }
      case 'busiest':
        return { what: `Busiest day ever: ${nf.format(e.count)} different aircraft${date === today ? ' so far' : ''}`,
          sub: `Old best ${nf.format(e.old_count)}${e.old_date ? ` on ${shortDay(e.old_date)}` : ''}` };
      case 'type':
        return { what: `First ${name(a) || a.type}${a.model && a.type ? ` (${a.type})` : ''} seen`,
          sub: [who(a), a.operator].filter(Boolean).join(', ') };
      case 'airline':
        return { what: `First ${e.airline || e.code} flight`,
          sub: [a.callsign, a.registration, name(a)].filter(Boolean).join(', ') };
      case 'country':
        return { what: `First aircraft from ${e.country}`,
          sub: `${who(a)}${name(a) ? ` (${name(a)})` : ''}${a.operator ? `, ${a.operator}` : ''}` };
      case 'milestone':
        return { what: `${nf.format(e.number)}th different aircraft`,
          sub: a.hex ? `${who(a)}${name(a) ? ` (${name(a)})` : ''}` : '' };
    }
    return { what: '', sub: '' };
  }

  // a day's events in kind groups (SkyStats sends them in order: records,
  // firsts, milestones), and its one-line summary
  function groups(events) {
    const out = [];
    for (const e of events) {
      if (!KINDS[e.kind]) continue;
      const last = out[out.length - 1];
      if (last && last.kind === e.kind) last.events.push(e); else out.push({ kind: e.kind, events: [e] });
    }
    return out;
  }
  function summary(events) {
    const n = { record: 0, first: 0, milestone: 0 };
    for (const e of events) {
      if (e.kind === 'range' || e.kind === 'busiest') n.record++;
      else if (e.kind === 'type' || e.kind === 'airline' || e.kind === 'country') n.first++;
      else if (e.kind === 'milestone') n.milestone++;
    }
    return [
      n.record && `${n.record} ${n.record === 1 ? 'record' : 'records'}`,
      n.first && `${n.first} ${n.first === 1 ? 'first' : 'firsts'}`,
      n.milestone && `${n.milestone} ${n.milestone === 1 ? 'milestone' : 'milestones'}`,
    ].filter(Boolean).join(', ');
  }

  function dayTitle(s) {
    if (s === today) return 'Today';
    if (today && (dayDate(today) - dayDate(s)) / 86400000 === 1) return 'Yesterday';
    return shortDay(s); // short, so every day's summary lines up
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
  function toggleDay(date) { openDays[date] = !openDays[date]; }
  function toggleKind(key) { openKinds[key] = !openKinds[key]; }
</script>

<section class="dl-card ne">
  <div class="dl-card-head">
    <h2 class="dl-h2">Notable events</h2>
    <span class="dl-note">New records, firsts and milestones</span>
  </div>
  {#if loading}
    <div class="skeleton h-16 w-full mt-4"></div>
  {:else if error}
    <p class="dl-note mt-3">{error}</p>
  {:else if !days.length}
    <p class="dl-note mt-3">Nothing yet. New range records, the busiest day so far, and aircraft types, airlines and
      countries seen for the first time show up here once your station has a week of history; round numbers of
      different aircraft show up from the start.</p>
  {:else}
    <ul class="ne-days">
      {#each days as d (d.date)}
        {@const isOpen = !!openDays[d.date]}
        <li>
          <button type="button" class="ne-day" aria-expanded={isOpen} on:click={() => toggleDay(d.date)}>
            <span class="ne-day-name">{dayTitle(d.date)}</span>
            <span class="ne-day-sum">{summary(d.events)}</span>
            <span class="ne-chev" class:ne-chev-open={isOpen} aria-hidden="true"></span>
          </button>
          {#if isOpen}
            <ul class="ne-list">
              {#each groups(d.events) as g}
                {@const key = d.date + '|' + g.kind}
                {@const extra = g.events.length - 1}
                {#each g.events as e, i}
                  {#if i === 0 || openKinds[key]}
                    {@const w = words(e, d.date)}
                    <li>
                      <button type="button" class="ne-row" class:ne-link={e.aircraft && e.aircraft.hex} on:click={() => open(e)}
                        title={e.aircraft && e.aircraft.hex ? `Search for ${who(e.aircraft)}` : ''}>
                        <span class="ne-tag ne-{KINDS[e.kind][1]}">{KINDS[e.kind][0]}</span>
                        <span class="ne-text">
                          <span class="ne-what">{w.what}</span>
                          {#if w.sub}<span class="ne-sub">{w.sub}</span>{/if}
                        </span>
                        <span class="ne-when dl-mono">{time(e.when)}</span>
                      </button>
                    </li>
                  {/if}
                {/each}
                {#if extra > 0}
                  <li class="ne-more">
                    <button type="button" class="ne-more-btn" aria-expanded={!!openKinds[key]} on:click={() => toggleKind(key)}>
                      {openKinds[key] ? 'Hide' : 'Show'} {extra} more {extra === 1 ? KINDS[g.kind][2] : KINDS[g.kind][3]}
                      <span class="ne-chev ne-chev-sm" class:ne-chev-open={openKinds[key]} aria-hidden="true"></span>
                    </button>
                  </li>
                {/if}
              {/each}
            </ul>
          {/if}
        </li>
      {/each}
    </ul>
    {#if more}
      <button type="button" class="btn btn-sm btn-ghost mt-2" on:click={loadOlder} disabled={loadingOlder}>
        {loadingOlder ? 'Loading…' : 'Show older days'}
      </button>
    {/if}
  {/if}
</section>

<style>
  .ne { margin-top: 2.5rem; }
  .ne-days { list-style: none; margin: 10px 0 0; padding: 0; }
  .ne-days > li { border-top: 1px solid var(--dl-line-2); }
  .ne-days > li:first-child { border-top: 0; }
  .ne-day { width: 100%; display: flex; align-items: center; gap: 12px; padding: 12px 0; border: 0; background: none;
    color: inherit; font: inherit; text-align: left; cursor: pointer; }
  .ne-day-name { font-weight: 700; min-width: 7.5rem; }
  .ne-day-sum { flex: 1; font-size: 13px; color: var(--dl-muted); }
  .ne-day:hover .ne-day-name { color: rgb(var(--dl-accent-rgb)); }
  .ne-chev { width: 8px; height: 8px; border-right: 2px solid var(--dl-muted); border-bottom: 2px solid var(--dl-muted);
    transform: translateY(-2px) rotate(45deg); transition: transform 0.15s; flex: none; margin-right: 4px; }
  .ne-chev-open { transform: translateY(2px) rotate(-135deg); }
  .ne-chev-sm { width: 6px; height: 6px; border-color: currentColor; display: inline-block; margin-left: 6px; }
  .ne-list { list-style: none; margin: 0 0 10px; padding: 0; }
  .ne-row { width: 100%; display: grid; grid-template-columns: 92px minmax(0, 1fr) auto; gap: 12px; align-items: center;
    padding: 9px 0; border: 0; border-top: 1px solid var(--dl-line-2); background: none; color: inherit; text-align: left;
    font: inherit; cursor: default; }
  .ne-list > li:first-child .ne-row { border-top: 0; padding-top: 2px; }
  .ne-link { cursor: pointer; }
  .ne-link:hover .ne-what { color: rgb(var(--dl-accent-rgb)); }
  .ne-tag { justify-self: start; font-size: 12px; font-weight: 700; padding: 2px 10px; border-radius: 999px; white-space: nowrap; }
  /* the same colours as the feeder's notable events card */
  .ne-rec { background: var(--dl-mil-soft); color: var(--dl-mil-ink); }
  .ne-new { background: var(--dl-accent-soft); color: var(--dl-accent-ink); }
  .ne-mile { background: rgba(22, 163, 74, 0.14); color: #15803d; }
  :global([data-theme='daylight-dark']) .ne-mile { background: rgba(74, 222, 128, 0.14); color: #4ade80; }
  .ne-text { display: flex; flex-direction: column; min-width: 0; }
  .ne-what { font-weight: 600; line-height: 1.3; }
  .ne-sub { font-size: 13px; color: var(--dl-muted); line-height: 1.3; margin-top: 2px; }
  .ne-when { font-size: 13px; color: var(--dl-muted); white-space: nowrap; }
  .ne-more { padding: 0 0 6px 104px; }
  .ne-more-btn { border: 0; background: none; padding: 2px 0; color: rgb(var(--dl-accent-rgb)); font: inherit; font-size: 13px;
    font-weight: 600; cursor: pointer; display: inline-flex; align-items: center; }
  .ne-more-btn:hover { text-decoration: underline; }
  @media (max-width: 640px) {
    .ne-day { flex-wrap: wrap; gap: 2px 10px; }
    .ne-day-name { min-width: 0; }
    .ne-day-sum { flex-basis: 100%; order: 3; }
    .ne-chev { margin-left: auto; }
    .ne-row { grid-template-columns: minmax(0, 1fr) auto; gap: 4px 10px; }
    .ne-tag { grid-column: 1 / -1; }
    .ne-more { padding-left: 0; }
  }
</style>
