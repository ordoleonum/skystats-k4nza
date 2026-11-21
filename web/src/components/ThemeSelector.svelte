<script>
  // Light / dark switch. The feeder opens SkyStats with ?k4theme=light|dark so
  // both start in the same look; after that the choice is remembered here.
  import { onMount } from 'svelte';
  import { notifyThemeChange } from '../lib/themeStore.js';

  const LIGHT = 'daylight';
  const DARK = 'daylight-dark';
  // themes saved by earlier versions
  const OLD = { nord: LIGHT, night: DARK };

  let dark = false;

  function apply(theme, save) {
    dark = theme === DARK;
    document.documentElement.setAttribute('data-theme', theme);
    if (save) {
      try { localStorage.setItem('theme', theme); } catch (e) { /* private window */ }
    }
    notifyThemeChange();
  }

  onMount(() => {
    let theme = null;
    const fromFeeder = new URLSearchParams(window.location.search).get('k4theme');
    if (fromFeeder === 'light' || fromFeeder === 'dark') {
      theme = fromFeeder === 'dark' ? DARK : LIGHT;
      apply(theme, true);
      // tidy the address so a bookmark doesn't keep forcing it
      try {
        const url = new URL(window.location.href);
        url.searchParams.delete('k4theme');
        history.replaceState(null, '', url);
      } catch (e) { /* ignore */ }
      return;
    }
    try { theme = localStorage.getItem('theme'); } catch (e) { /* private window */ }
    theme = OLD[theme] || theme;
    if (theme !== LIGHT && theme !== DARK) {
      theme = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? DARK : LIGHT;
    }
    apply(theme, false);
  });

  function toggle() {
    apply(dark ? LIGHT : DARK, true);
  }
</script>

<button type="button" class="dl-btn dl-icon-btn" on:click={toggle}
  aria-label={dark ? 'Switch to light mode' : 'Switch to dark mode'}
  title={dark ? 'Switch to light mode' : 'Switch to dark mode'}>
  {#if dark}
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><circle cx="12" cy="12" r="4"></circle><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"></path></svg>
  {:else}
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z"></path></svg>
  {/if}
</button>
