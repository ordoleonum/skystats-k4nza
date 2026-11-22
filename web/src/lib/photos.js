// Wikipedia's photo of an aircraft TYPE, for aircraft with no photo of their
// own (shown clearly as "Typical ..., not this aircraft"). Tries each name in
// turn (full model name first, ICAO code last). A result is only used if
// Wikipedia describes it as an aircraft (bare codes also find engines, ships
// and lists: T38 is also an engine) and its picture is a photo (JPEG), not a
// drawing or silhouette.
// whole words only: "airline" must not match "Airliner"
const WIKI_NOT = /\b(list of|disambiguation|airports?|airlines?|company|manufacturer|ships?|cars?|vehicles?|facility|factory|plant|assembly|museum|base|squadron|crash|accident|incident|disaster|category)\b/i;
// A page counts as an aircraft when its description names an aircraft and no
// engine ("single engine aircraft" and "twin-engine helicopter" yes, "turbojet aircraft engine" no).
// Among those, a page whose title is part of the searched name wins (UH-60 Black
// Hawk over HH-60 Pave Hawk), else the first one Wikipedia ranks.
const WIKI_AIRCRAFT = /aircraft|airliner|trainer|fighter|helicopter|bomber|tanker|airlifter|airplane|aeroplane|jet|plane|glider|gunship/i;
function wikiPick(pages, name) {
  const key = (s) => String(s || "").toLowerCase().replace(/\s*\(.*\)$/, "").replace(/[^a-z0-9]/g, "");
  const ok = pages.filter((pg) => {
    const d = pg.description || "";
    const engine = /\bengines?\b/i.test(d.replace(/\b(single|twin|multi|two|three|four|six|eight)[- ]engines?\b/gi, ""));
    return pg.thumbnail && /\.jpe?g/i.test(pg.thumbnail.source) && WIKI_AIRCRAFT.test(d) && !engine &&
      !WIKI_NOT.test(d + " " + pg.title);
  });
  return ok.find((pg) => key(pg.title).length >= 3 && key(name).includes(key(pg.title))) || ok[0] || null;
}

// type name -> page found (or null for none): each type is asked once per page load
const wikiCache = new Map();

export async function wikiTypePhoto(names, typical) {
    for (const name of [...new Set(names.map((n) => (n || '').trim()).filter(Boolean))]) {
        try {
            let page = wikiCache.get(name);
            if (page === undefined) {
                const url = 'https://en.wikipedia.org/w/api.php?action=query&generator=search&gsrlimit=5' +
                    '&prop=pageimages|description&piprop=thumbnail&pithumbsize=400&format=json&origin=*&gsrsearch=' +
                    encodeURIComponent(`${name} aircraft`);
                const res = await fetch(url, { signal: AbortSignal.timeout(15000) });
                if (!res.ok) return null;
                const pages = Object.values((await res.json()).query?.pages || {}).sort((a, b) => a.index - b.index);
                page = wikiPick(pages, name);
                wikiCache.set(name, page);
            }
            if (page) {
                return {
                    src: page.thumbnail.source,
                    link: `https://en.wikipedia.org/wiki/${encodeURIComponent(page.title.replace(/ /g, '_'))}`,
                    photographer: 'Wikipedia',
                    typical: typical || name,
                };
            }
        } catch (e) {
            return null; // offline, blocked or too slow: the outline is shown
        }
    }
    return null;
}
