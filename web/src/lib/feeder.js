// Links back to the feeder that SkyStats runs on: its home page, and the live
// map's replay of a past visit.
import { writable } from 'svelte/store';

// the feeder's setup app answers on the standard port of the same host
export function feederBase() {
    return `${window.location.protocol}//${window.location.hostname}/`;
}

function currentTheme() {
    return document.documentElement.getAttribute('data-theme') === 'daylight-dark' ? 'dark' : 'light';
}

// tar1090 replay: ?replay=YYYY-MM-DD-HH:MM (UTC, in 5-minute steps) starts the
// replay there; &icao= selects the aircraft. The start is rounded down so the
// replay begins just before the aircraft appeared.
export function replayUrl(hex, start) {
    const d = new Date(start);
    if (!hex || isNaN(d)) return null;
    d.setUTCSeconds(0, 0);
    d.setUTCMinutes(Math.floor(d.getUTCMinutes() / 5) * 5);
    const p = (n) => String(n).padStart(2, '0');
    const stamp = `${d.getUTCFullYear()}-${p(d.getUTCMonth() + 1)}-${p(d.getUTCDate())}-${p(d.getUTCHours())}:${p(d.getUTCMinutes())}`;
    return `${feederBase()}map/?replay=${stamp}&icao=${encodeURIComponent(String(hex).toLowerCase())}&k4theme=${currentTheme()}`;
}

// The Feeder button: SkyStats is opened in its own tab from the feeder, so
// closing the tab lands back there. When the browser won't close it, go back,
// or open the feeder in this tab.
export function backToFeeder(e) {
    if (e.ctrlKey || e.metaKey || e.shiftKey || e.button === 1) return; // new tab / window: follow the link
    e.preventDefault();
    let opener = null;
    try { opener = window.opener && !window.opener.closed ? window.opener : null; } catch (err) { /* cross-origin */ }
    if (opener || history.length <= 1) {
        if (opener) {
            try { opener.focus(); } catch (err) { /* not allowed: closing still returns to it */ }
        }
        window.close();
    } else if (history.length > 1 && document.referrer.startsWith(feederBase())) {
        history.back();
        return;
    }
    setTimeout(() => { if (!window.closed) window.location.href = feederBase(); }, 300);
}

// station name and feeder version for the header (from the feeder's settings)
export const feederInfo = writable({ station_name: '', feeder_version: '' });
fetch('api/config')
    .then((r) => (r.ok ? r.json() : {}))
    .then((c) => feederInfo.set({ station_name: c.station_name || '', feeder_version: c.feeder_version || '' }))
    .catch(() => {});
