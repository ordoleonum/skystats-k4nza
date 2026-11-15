// Opens the Search tab and searches for the given text (e.g. a registration
// clicked in Interesting Aircraft or Record Holders). App.svelte switches the
// tab; TabSearch.svelte runs the search.
export function searchFor(text) {
    const q = String(text || '').trim();
    if (!q) return;
    window.dispatchEvent(new CustomEvent('skystats-search-for', { detail: q }));
}
