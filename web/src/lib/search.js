// Opens the Search tab and searches for the given text (e.g. a registration
// clicked in Interesting Aircraft or Record Holders). App.svelte switches the
// tab; TabSearch.svelte runs the search.
export function searchFor(text) {
    const q = String(text || '').trim();
    if (!q) return;
    window.dispatchEvent(new CustomEvent('skystats-search-for', { detail: q }));
}

// Opens the Search tab listing every aircraft of one interesting group ever
// seen (group: Mil, Gov, Pol or Civ), e.g. from a group card header.
export function searchGroup(group) {
    if (!group) return;
    window.dispatchEvent(new CustomEvent('skystats-search-for', { detail: { group } }));
}
