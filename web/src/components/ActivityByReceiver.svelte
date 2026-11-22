<script>
  // Radar Nodes: the whole network split by receiver (Activity tab, Full network only).
  // Each receiver's own SkyStats answers for itself at r/<id>/api/k4nza/report.
  import { receivers, receiverUrl } from '../lib/receiver.js';
  import { units } from '../stores/units';
  import { fromNm, distanceLabel } from '../lib/units';

  export let period = 'week';
  export let total = 0; // flights in the whole network for this period

  let rows = [];
  let loadedFor = '';

  $: others = $receivers.filter((r) => r.id !== 'all');
  $: key = period + '|' + others.map((r) => r.id).join(',');
  $: if (others.length && key !== loadedFor) load(key);

  async function load(k) {
    loadedFor = k;
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
    const list = others.map((r) => ({ ...r, flights: null, aircraft: null, far: null, failed: false }));
    rows = list;
    await Promise.all(list.map(async (r) => {
      try {
        const res = await fetch(`r/${r.id}/api/k4nza/report?period=${period}&tz=${encodeURIComponent(tz)}`);
        if (!res.ok) throw new Error(res.status);
        const d = await res.json();
        r.flights = d.flights || 0;
        r.aircraft = d.aircraft || 0;
        r.far = d.farthest && d.farthest.distance_nm ? d.farthest.distance_nm : null;
      } catch (e) {
        r.failed = true;
      }
    }));
    if (loadedFor === k) rows = [...list];
  }

  const num = (n) => (n == null ? '–' : Number(n).toLocaleString());
  $: dist = (nm) => (nm == null || !$units ? '–' : `${Math.round(fromNm(nm, $units)).toLocaleString()} ${distanceLabel[$units]}`);
  $: max = Math.max(1, ...rows.map((r) => r.flights || 0));
</script>

{#if others.length}
  <section class="dl-card rep-recv">
    <div class="dl-card-head rep-mb">
      <h2 class="dl-h2">By receiver</h2>
      <span class="dl-note">Pick one under Activity to see only its stats</span>
    </div>
    <div class="rep-recv-scroll">
      <table class="rep-recv-table">
        <thead>
          <tr><th>Receiver</th><th class="num">Flights</th><th class="num">Different aircraft</th><th class="num">Farthest</th><th class="num">Share of network</th></tr>
        </thead>
        <tbody>
          {#each rows as r}
            <tr>
              <td>
                <a class="dl-link" href={receiverUrl(r.id)}>{r.name}</a>{#if r.paused}<span class="dl-note"> (paused)</span>{/if}
                <span class="rep-bar-track rep-recv-bar"><span class="rep-bar" style="width: {Math.max(2, Math.round((r.flights || 0) / max * 100))}%"></span></span>
              </td>
              {#if r.failed}
                <td class="num dl-note" colspan="4">Not answering yet (starting, or no data)</td>
              {:else}
                <td class="num dl-mono">{num(r.flights)}</td>
                <td class="num dl-mono">{num(r.aircraft)}</td>
                <td class="num dl-mono">{dist(r.far)}</td>
                <td class="num dl-mono">{r.flights == null || !total ? '–' : Math.round(r.flights / total * 100) + '%'}</td>
              {/if}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <p class="dl-note rep-foot">Shares add up to more than 100% when several receivers hear the same flight.</p>
  </section>
{/if}

<style>
  .rep-recv-scroll { overflow-x: auto; }
  .rep-recv-table { width: 100%; border-collapse: collapse; font-size: 14px; min-width: 520px; }
  .rep-recv-table th { text-align: left; font-size: 12px; color: var(--dl-muted); font-weight: 600; padding: 8px 10px; border-bottom: 1px solid var(--dl-line); white-space: nowrap; }
  .rep-recv-table td { padding: 10px; border-bottom: 1px solid var(--dl-line-2); vertical-align: middle; }
  .rep-recv-table .num { text-align: right; font-variant-numeric: tabular-nums; }
  .rep-recv-bar { display: block; margin-top: 6px; max-width: 220px; }
</style>
