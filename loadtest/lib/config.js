// Settings shared by every script. Everything comes from the environment so the same scripts run locally, in CI and
// against staging (k6: -e BASE_URL=...).
export const BASE_URL = (__ENV.BASE_URL || 'http://127.0.0.1:8085').replace(/\/$/, '');
export const API = `${BASE_URL}/api/v1/fupi`;
export const SEED_FILE = __ENV.SEED_FILE || '/loadtest/.data/seed.json';

export const JSON_HEADERS = { 'Content-Type': 'application/json' };

export function bearer(token) {
  return { headers: { ...JSON_HEADERS, Authorization: `Bearer ${token}` } };
}

function seconds(duration) {
  const m = /^(\d+)(s|m|h)$/.exec(duration);
  return parseInt(m[1], 10) * { s: 1, m: 60, h: 3600 }[m[2]];
}

// maxDropped is the dropped-iteration budget of an arrival-rate scenario: DROP_TOLERANCE (default 1%) of the iterations
// it should run. A dropped iteration means the generator had no free VU, which on a busy laptop also happens when the
// generator itself stalls, so a strict zero would fail runs whose server side was fine.
export function maxDropped(rate, duration) {
  return Math.ceil(parseFloat(__ENV.DROP_TOLERANCE || '0.01') * rate * seconds(duration));
}
