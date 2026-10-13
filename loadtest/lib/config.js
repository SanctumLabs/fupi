// Settings shared by every script. Everything comes from the environment so the same scripts run locally, in CI and
// against staging (k6: -e BASE_URL=...).
export const BASE_URL = (__ENV.BASE_URL || 'http://127.0.0.1:8085').replace(/\/$/, '');
export const API = `${BASE_URL}/api/v1/fupi`;
export const SEED_FILE = __ENV.SEED_FILE || '/loadtest/.data/seed.json';

export const JSON_HEADERS = { 'Content-Type': 'application/json' };

export function bearer(token) {
  return { headers: { ...JSON_HEADERS, Authorization: `Bearer ${token}` } };
}
