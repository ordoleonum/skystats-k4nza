<script>
  import { IconSettings, IconSearch } from '@tabler/icons-svelte';
  import ThemeSelector from './components/ThemeSelector.svelte';
  import TabRouteStats from './components/TabRouteStats.svelte';
  import TabMotionStats from './components/TabMotionStats.svelte';
  import TabInterestingStats from './components/TabInterestingStats.svelte';
  import TabActivity from './components/TabActivity.svelte';
  import TabSearch from './components/TabSearch.svelte';
  import Footer from './components/Footer.svelte';
  import Settings from './components/Settings.svelte';
  import { feederBase, backToFeeder, feederInfo } from './lib/feeder.js';
  import { receiverId, receivers, loadReceivers, receiverUrl, receiverName } from './lib/receiver.js';
  import { onMount } from 'svelte';

  // ?q=... (a search, e.g. from the feeder home page) or ?group=Mil|Gov|Pol|Civ
  // (a whole interesting group) opens the Search tab
  const startParams = new URLSearchParams(window.location.search);
  let activeTab = startParams.get('q') || startParams.get('group') ? 'search' : 'activity';

  const tabs = [
    { name: 'activity', label: 'Activity', component: TabActivity },
    { name: 'route-stat', label: 'Routes', component: TabRouteStats },
    { name: 'interesting-stat', label: 'Interesting', component: TabInterestingStats },
    { name: 'motion-stat', label: 'Records', component: TabMotionStats },
    { name: 'search', label: 'Search', component: TabSearch }
  ];

  function setActiveTab(tabName) {
    activeTab = tabName;
    window.scrollTo({ top: 0 });
  }

  // the SkyStats title: back to the first tab, top of the page, no ?q= / ?group= left
  function goHome() {
      activeTab = 'activity';
      try {
          const url = new URL(window.location.href);
          url.searchParams.delete('q');
          url.searchParams.delete('group');
          history.replaceState(null, '', url);
      } catch (e) { /* ignore */ }
      window.scrollTo({ top: 0 });
  }

  function openSettingsModal() {
      document.getElementById("settings-modal").showModal();
  }

  function openSearch() {
      setActiveTab('search');
      window.dispatchEvent(new Event('skystats-open-search'));
  }

  // a registration clicked in another tab (lib/search.js); TabSearch runs the search
  window.addEventListener('skystats-search-for', () => setActiveTab('search'));
  // "All interesting aircraft" on the Activity tab
  window.addEventListener('skystats-open-tab', (e) => setActiveTab(e.detail));

  // Radar Nodes: the Activity tab becomes a menu of receivers once the feeder has nodes
  let receiverMenuOpen = false;
  onMount(() => {
      loadReceivers();
      const close = (e) => {
          if (!e.target.closest || !e.target.closest('.dl-recv')) receiverMenuOpen = false;
      };
      const esc = (e) => { if (e.key === 'Escape') receiverMenuOpen = false; };
      document.addEventListener('click', close);
      document.addEventListener('keydown', esc);
      return () => { document.removeEventListener('click', close); document.removeEventListener('keydown', esc); };
  });
  function pickReceiver(id) {
      receiverMenuOpen = false;
      if (id === receiverId) { setActiveTab('activity'); return; }
      window.location.assign(receiverUrl(id));
  }
  $: receiverLabel = receiverId === 'all' ? '' : receiverName($receivers, receiverId);

  // "k4nza build -34", as in the feeder's top bar: the K4NZA release number only
  $: release = ($feederInfo.feeder_version.match(/-(\d+)\s*$/) || [])[1];
  $: tagline = 'k4nza build' + (release ? ' -' + release : '');
</script>

<header class="dl-header">
  <div class="dl-header-in">
    <div class="dl-brand">
      <a class="dl-btn dl-back" href={feederBase()} on:click={backToFeeder} title="Back to the feeder" aria-label="Back to the feeder">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 6l-6 6 6 6"></path></svg><span>Feeder</span>
      </a>
      <a class="dl-title" href="./" title="Back to the first tab" on:click|preventDefault={goHome}>
        <span class="dl-title-main">SkyStats{#if $feederInfo.station_name}{' '}<span class="dl-station">{$feederInfo.station_name}</span>{/if}</span>
        <span class="dl-tagline dl-mono">{tagline}</span>
      </a>
    </div>
    <div class="dl-header-tools">
      <nav aria-label="SkyStats sections" class="dl-nav">
        {#each tabs as tab}
          {#if tab.name === 'activity' && $receivers.length}
            <div class="dl-recv">
              <button type="button" class="dl-tab" aria-current={activeTab === tab.name ? 'page' : undefined}
                aria-haspopup="menu" aria-expanded={receiverMenuOpen}
                on:click={() => receiverMenuOpen = !receiverMenuOpen}>
                Activity{#if receiverLabel}<span class="dl-recv-name">· {receiverLabel}</span>{/if}
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M6 9l6 6 6-6"></path></svg>
              </button>
              {#if receiverMenuOpen}
                <div class="dl-recv-menu" role="menu">
                  <div class="dl-recv-head">Show activity for</div>
                  {#each $receivers as r, i}
                    <button type="button" role="menuitemradio" aria-checked={r.id === receiverId}
                      class="dl-recv-item" on:click={() => pickReceiver(r.id)}>
                      <span>{r.name}{#if r.paused}<span class="dl-recv-note"> (paused)</span>{/if}</span>
                      {#if r.id === receiverId}<span aria-hidden="true">✓</span>{/if}
                    </button>
                    {#if i === 1}<div class="dl-recv-sep"></div>{/if}
                  {/each}
                </div>
              {/if}
            </div>
          {:else}
          <button type="button" class="dl-tab" aria-current={activeTab === tab.name ? 'page' : undefined}
            on:click={() => tab.name === 'search' ? openSearch() : setActiveTab(tab.name)}>
            {#if tab.name === 'search'}<IconSearch class="h-4 w-4" />{/if}{tab.label}
          </button>
          {/if}
        {/each}
      </nav>
      <div class="dl-header-btns">
        <button type="button" class="dl-btn dl-icon-btn" on:click={openSettingsModal} aria-label="Settings" title="Display settings and about">
          <IconSettings class="h-5 w-5" />
        </button>
        <ThemeSelector/>
      </div>
    </div>
  </div>
</header>

<main class="dl-main">
  {#each tabs as tab}
    <div class="{activeTab === tab.name ? 'block fade-in' : 'hidden'}">
      <svelte:component this={tab.component} active={activeTab === tab.name} />
    </div>
  {/each}

  <Footer />
</main>

<Settings />

<style>
  .dl-header { background: var(--dl-surface); border-bottom: 1px solid var(--dl-line); }
  .dl-recv { position: relative; }
  .dl-recv-name { color: var(--dl-accent); font-weight: 700; }
  .dl-recv-menu {
    position: absolute; top: calc(100% + 6px); left: 0; z-index: 40; min-width: 230px;
    background: var(--dl-surface); border: 1px solid var(--dl-line); border-radius: 14px; padding: 6px;
    box-shadow: 0 16px 40px rgba(15, 23, 42, 0.16); display: flex; flex-direction: column; gap: 1px;
  }
  .dl-recv-head { font-size: 11px; text-transform: uppercase; letter-spacing: .08em; color: var(--dl-muted); font-weight: 700; padding: 6px 12px 4px; }
  .dl-recv-item {
    display: flex; justify-content: space-between; align-items: center; gap: 12px; min-height: 40px; padding: 0 12px;
    border: 0; border-radius: 10px; background: transparent; color: var(--dl-ink); font: inherit; font-weight: 600; text-align: left; cursor: pointer;
  }
  .dl-recv-item:hover { background: var(--dl-surface-2); }
  .dl-recv-item[aria-checked="true"] { background: var(--dl-accent-soft); color: var(--dl-accent-ink); }
  .dl-recv-item:focus-visible { outline: 2px solid var(--dl-accent); outline-offset: 1px; }
  .dl-recv-note { color: var(--dl-muted); font-weight: 400; }
  .dl-recv-sep { height: 1px; background: var(--dl-line); margin: 4px 6px; }
  .dl-header-in {
    max-width: 1180px;
    margin: 0 auto;
    padding: 10px 24px;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .dl-brand { display: flex; align-items: center; gap: 12px; min-width: 0; }
  .dl-back { flex: none; padding: 0 14px 0 10px; }
  .dl-title { display: flex; flex-direction: column; line-height: 1.15; min-width: 0; color: var(--dl-ink); text-decoration: none; }
  .dl-title-main { font-weight: 800; font-size: 18px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .dl-station { font-weight: 600; font-size: 13px; color: var(--dl-muted); }
  .dl-tagline { font-size: 11px; font-weight: 500; color: var(--dl-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .dl-header-tools { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
  .dl-nav { display: flex; flex-wrap: wrap; gap: 2px; }
  .dl-header-btns { display: flex; gap: 6px; }
  .dl-main {
    max-width: 1180px;
    margin: 0 auto;
    padding: 36px 24px 40px;
  }

  /* phones: the sections scroll sideways under the title */
  @media (max-width: 760px) {
    .dl-header-in { padding: 10px 16px; }
    .dl-header-tools { display: contents; }
    .dl-brand { flex: 1 1 0; }
    .dl-nav { order: 3; width: 100%; flex-wrap: nowrap; overflow-x: auto; scrollbar-width: none; min-width: 0; }
    .dl-nav::-webkit-scrollbar { display: none; }
    .dl-main { padding: 24px 16px 32px; }
  }
  @media (max-width: 420px) {
    .dl-back span { display: none; }
    .dl-back { width: 44px; padding: 0; }
  }

  .fade-in {
    animation: fadeIn 0.3s ease-in;
  }

  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  /* Override the divider for DaisyUI list component, as its stopped working in recent versions */
  :global(.soft-divider > :not(:last-child).list-row)::after,
  :global(.soft-divider > :not(:last-child) .list-row)::after {
    opacity: 0.05 !important;
  }
</style>
