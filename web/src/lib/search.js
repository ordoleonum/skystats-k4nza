// Opens the Search tab and searches for the given text (e.g. a registration
// clicked in Interesting Aircraft or Record Holders). App.svelte switches the
// tab; TabSearch.svelte runs the search.
export function searchFor(text) {
    const q = String(text || '').trim();
    if (!q) return;
    window.dispatchEvent(new CustomEvent('skystats-search-for', { detail: q }));
}

// Same search, but in a new browser tab (?q=, keeping the light/dark theme),
// so the page the user clicked from stays where it was.
export function searchInNewTab(text) {
    const q = String(text || '').trim();
    if (!q) return;
    const url = new URL(window.location.href);
    url.search = '';
    url.hash = '';
    url.searchParams.set('q', q);
    const dark = document.documentElement.getAttribute('data-theme') === 'daylight-dark';
    url.searchParams.set('k4theme', dark ? 'dark' : 'light');
    window.open(url.toString(), '_blank');
}

// Opens the Search tab listing every aircraft of one interesting group ever
// seen (group: Mil, Gov, Pol or Civ), e.g. from a group card header.
export function searchGroup(group) {
    if (!group) return;
    window.dispatchEvent(new CustomEvent('skystats-search-for', { detail: { group } }));
}
