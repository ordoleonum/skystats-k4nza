// Radar Nodes: which receiver this page shows.
//
// ?receiver=<id> picks one receiver (this station's radio "home", or a node);
// without it the page shows the whole network as before. Each receiver has
// its own SkyStats behind /r/<id>/, so data requests ("api/...") are sent
// there. Settings, the feeder info, the version and exports stay with this
// SkyStats. Switching receiver reloads the page.

import { writable } from 'svelte/store';

const params = new URLSearchParams(window.location.search);
const asked = (params.get('receiver') || '').toLowerCase();
export const receiverId = /^[a-z0-9-]{1,40}$/.test(asked) && asked !== 'all' ? asked : 'all';

// [{id, name, paused}] from the feeder; empty without nodes (then there's no menu)
export const receivers = writable([]);

const KEEP_HERE = /^\/?api\/(settings|config|version|export|k4nza\/receivers)\b/;

if (receiverId !== 'all') {
  const plainFetch = window.fetch.bind(window);
  window.fetch = (input, init) => {
    if (typeof input === 'string') {
      const path = input.replace(/^\//, '');
      if (path.startsWith('api/') && !KEEP_HERE.test(input)) {
        input = `r/${receiverId}/${path}`;
      }
    }
    return plainFetch(input, init);
  };
}

export function loadReceivers() {
  // the real fetch path is never rewritten (KEEP_HERE)
  fetch('api/k4nza/receivers', { cache: 'no-store' })
    .then((r) => (r.ok ? r.json() : { receivers: [] }))
    .then((d) => receivers.set(Array.isArray(d.receivers) ? d.receivers : []))
    .catch(() => receivers.set([]));
}

// the address for another receiver: the Activity tab, no search left over
export function receiverUrl(id) {
  const url = new URL(window.location.href);
  url.searchParams.delete('q');
  url.searchParams.delete('group');
  if (id === 'all') url.searchParams.delete('receiver');
  else url.searchParams.set('receiver', id);
  return url.pathname + url.search;
}

export function receiverName(list, id) {
  const r = list.find((x) => x.id === id);
  return r ? r.name : id;
}
