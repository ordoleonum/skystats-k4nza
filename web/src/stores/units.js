// Units used to display distances, speeds and altitudes: 'nautical', 'imperial' or 'metric'.
// The choice is remembered in this browser. Until one is made, the feeder's default
// (DEFAULT_UNITS, from /api/config) is used.
import { writable } from 'svelte/store';

const KEY = 'skystats-units';
export const UNIT_SYSTEMS = ['nautical', 'imperial', 'metric'];

function stored() {
    try {
        const v = localStorage.getItem(KEY);
        return UNIT_SYSTEMS.includes(v) ? v : null;
    } catch (e) {
        return null;
    }
}

export const units = writable(stored() || 'nautical');

export function setUnits(value) {
    if (!UNIT_SYSTEMS.includes(value)) return;
    try { localStorage.setItem(KEY, value); } catch (e) { /* private mode */ }
    units.set(value);
}

export async function loadDefaultUnits() {
    if (stored()) return;
    try {
        const res = await fetch('api/config');
        if (!res.ok) return;
        const cfg = await res.json();
        if (UNIT_SYSTEMS.includes(cfg.default_units)) units.set(cfg.default_units);
    } catch (e) { /* keep nautical */ }
}
