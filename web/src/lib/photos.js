// Wikipedia's photo of an aircraft TYPE, for aircraft with no photo of their
// own (shown clearly as "Typical ..., not this aircraft"). Tries each name in
// turn (full model name first, ICAO code last). A result is only used if
// Wikipedia describes it as an aircraft (bare codes also find engines, ships
// and lists: T38 is also an engine) and its picture is a photo (JPEG), not a
// drawing or silhouette.
const WIKI_AIRCRAFT = /aircraft|airliner|trainer|fighter|helicopter|bomber|tanker|airlifter|airplane|aeroplane|jet|plane|glider|gunship/i;
// whole words only: "airline" must not match "Airliner"
const WIKI_NOT = /\b(engine|list of|family of|disambiguation|airport|airline|company|manufacturer|ship|car|vehicle|facility|factory|plant|assembly|museum|base|squadron)\b/i;

export async function wikiTypePhoto(names, typical) {
    for (const name of [...new Set(names.map((n) => (n || '').trim()).filter(Boolean))]) {
        try {
            const url = 'https://en.wikipedia.org/w/api.php?action=query&generator=search&gsrlimit=5' +
                '&prop=pageimages|description&piprop=thumbnail&pithumbsize=400&format=json&origin=*&gsrsearch=' +
                encodeURIComponent(`${name} aircraft`);
            const res = await fetch(url);
            if (!res.ok) return null;
            const pages = Object.values((await res.json()).query?.pages || {}).sort((a, b) => a.index - b.index);
            const page = pages.find((p) => /\.jpe?g/i.test(p.thumbnail?.source || '') && WIKI_AIRCRAFT.test(p.description || '') &&
                !WIKI_NOT.test(`${p.description || ''} ${p.title}`));
            if (page) {
                return {
                    src: page.thumbnail.source,
                    link: `https://en.wikipedia.org/wiki/${encodeURIComponent(page.title.replace(/ /g, '_'))}`,
                    photographer: 'Wikipedia',
                    typical: typical || name,
                };
            }
        } catch (e) {
            return null; // offline or blocked: the outline is shown
        }
    }
    return null;
}
