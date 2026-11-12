// Conversions for display. Stored data is always km (distances from the
// station), nm (reception charts), knots (speeds) and feet (altitudes).

const KM_PER_NM = 1.852;
const KM_PER_MI = 1.609344;

export const distanceLabel = { nautical: 'nm', imperial: 'mi', metric: 'km' };
export const speedLabel = { nautical: 'kts', imperial: 'mph', metric: 'km/h' };
export const altitudeLabel = { nautical: 'ft', imperial: 'ft', metric: 'm' };

export function fromKm(km, system) {
    if (km === null || km === undefined || isNaN(km)) return km;
    km = Number(km);
    if (system === 'metric') return km;
    if (system === 'imperial') return km / KM_PER_MI;
    return km / KM_PER_NM;
}

export function fromNm(nm, system) {
    return fromKm(Number(nm) * KM_PER_NM, system);
}

// the inverse of fromNm: a value shown on a chart axis back to nm
export function toNm(value, system) {
    if (system === 'metric') return value / KM_PER_NM;
    if (system === 'imperial') return (value * KM_PER_MI) / KM_PER_NM;
    return value;
}

export function fromKnots(kt, system) {
    if (kt === null || kt === undefined || isNaN(kt)) return kt;
    kt = Number(kt);
    if (system === 'metric') return kt * KM_PER_NM;
    if (system === 'imperial') return (kt * KM_PER_NM) / KM_PER_MI;
    return kt;
}

export function fromFeet(ft, system) {
    if (ft === null || ft === undefined || isNaN(ft)) return ft;
    ft = Number(ft);
    return system === 'metric' ? ft * 0.3048 : ft;
}

export function round(value, decimals = 0) {
    const f = Math.pow(10, decimals);
    return Math.round(value * f) / f;
}

export function formatDistanceKm(km, system, decimals = 1) {
    return `${round(fromKm(km, system), decimals).toLocaleString()} ${distanceLabel[system]}`;
}

export function formatSpeed(kt, system) {
    return `${round(fromKnots(kt, system)).toLocaleString()} ${speedLabel[system]}`;
}

export function formatAltitude(ft, system) {
    return `${round(fromFeet(ft, system)).toLocaleString()} ${altitudeLabel[system]}`;
}
