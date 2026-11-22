// K4NZA: the station's time zone. Dates and times on every page are shown in it,
// and "today" starts at the station's midnight, wherever the viewing device is.
// The feeder sets it (STATION_TZ, sent as time_zone by /api/config); without it
// the device's own zone is used, as before.
const device = (() => {
    try { return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'; } catch (e) { return 'UTC'; }
})();

let zone = device;

export function stationTz() {
    return zone;
}

function valid(tz) {
    try { new Intl.DateTimeFormat('en-US', { timeZone: tz }); return true; } catch (e) { return false; }
}

// asked once before the page is drawn; a slow or missing answer keeps the device's zone
export async function loadStationTz() {
    try {
        const res = await fetch('api/config', { signal: AbortSignal.timeout(3000) });
        if (!res.ok) return zone;
        const cfg = await res.json();
        if (cfg.time_zone && valid(cfg.time_zone)) zone = cfg.time_zone;
    } catch (e) { /* keep the device's zone */ }
    return zone;
}

// today's date (YYYY-MM-DD) in the station's zone, stepped back `back` days
export function stationDate(back = 0) {
    const parts = {};
    new Intl.DateTimeFormat('en-US', { year: 'numeric', month: 'numeric', day: 'numeric', timeZone: zone })
        .formatToParts(new Date()).forEach((p) => { parts[p.type] = Number(p.value); });
    const d = new Date(Date.UTC(parts.year, parts.month - 1, parts.day - back));
    return d.toISOString().slice(0, 10);
}

// the station's calendar day (YYYY-MM-DD) of a moment
export function stationDay(d) {
    return new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit', timeZone: zone }).format(d);
}

// "Today", "Yesterday", "Nov 16" or "Nov 16, 2025", on the station's calendar
export function fmtStationDay(d) {
    const key = stationDay(d);
    if (key === stationDate(0)) return 'Today';
    if (key === stationDate(1)) return 'Yesterday';
    const thisYear = key.slice(0, 4) === stationDate(0).slice(0, 4);
    return d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: thisYear ? undefined : 'numeric', timeZone: zone });
}

// a clock time on the station's clock
export function fmtStationTime(d, opts = { hour: 'numeric', minute: '2-digit' }) {
    return d.toLocaleTimeString(undefined, { ...opts, timeZone: zone });
}
